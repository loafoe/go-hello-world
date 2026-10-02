package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestWantsHTML(t *testing.T) {
	tests := []struct {
		name   string
		accept string
		want   bool
	}{
		{"browser", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8", true},
		{"curl", "*/*", false},
		{"empty", "", false},
		{"json client", "application/json", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.accept != "" {
				r.Header.Set(echo.HeaderAccept, tt.accept)
			}
			if got := wantsHTML(r); got != tt.want {
				t.Errorf("wantsHTML() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHumanDuration(t *testing.T) {
	tests := []struct {
		in   time.Duration
		want string
	}{
		{0, "0s"},
		{500 * time.Millisecond, "0s"},
		{90 * time.Second, "1m30s"},
		{2*time.Hour + 3*time.Minute + 4*time.Second, "2h3m4s"},
	}

	for _, tt := range tests {
		if got := humanDuration(tt.in); got != tt.want {
			t.Errorf("humanDuration(%s) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestReadBuildInfo(t *testing.T) {
	bi := readBuildInfo()
	if bi.Version == "" {
		t.Error("version should never be empty")
	}
	if bi.GoVersion == "" {
		t.Error("go version should never be empty")
	}
	if bi.OS == "" || bi.Arch == "" {
		t.Errorf("platform should be set, got %s/%s", bi.OS, bi.Arch)
	}
}

func TestEmbeddedUI(t *testing.T) {
	if len(ui) == 0 {
		t.Fatal("ui/index.html is not embedded")
	}
	page := string(ui)
	if !strings.HasPrefix(strings.TrimSpace(page), "<!doctype html>") {
		t.Error("embedded UI should start with a doctype")
	}
	if !strings.Contains(page, `<link rel="icon" href="data:image/svg+xml`) {
		t.Error("favicon should be inlined, not fetched")
	}
	for _, forbidden := range []string{"https://", "http://cdn", "//cdn."} {
		if strings.Contains(page, forbidden) {
			t.Errorf("UI must be self contained, found external reference %q", forbidden)
		}
	}
}

func newTestServer() (*echo.Echo, *state) {
	st := newState()
	e := echo.New()
	e.HideBanner = true
	e.Use(countRequests(st))

	tracer := noop.NewTracerProvider().Tracer("test")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	bi := readBuildInfo()

	e.GET("/", hello(logger, tracer, "unit-test"))
	e.GET("/api/info", infoHandler(tracer, st, "unit-test", bi))
	e.GET("/healthz", healthz(st))

	return e, st
}

func TestHelloPlainText(t *testing.T) {
	e, _ := newTestServer()

	req := httptest.NewRequest(http.MethodGet, "/?foo=bar", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get(echo.HeaderContentType); !strings.HasPrefix(ct, echo.MIMETextPlain) {
		t.Errorf("content type = %q, want text/plain", ct)
	}
	want := `Hello from instance "unit-test"! You've requested: /?foo=bar`
	if got := strings.TrimSpace(rec.Body.String()); got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
}

func TestHelloServesUIForBrowsers(t *testing.T) {
	e, _ := newTestServer()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(echo.HeaderAccept, "text/html,application/xhtml+xml")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get(echo.HeaderContentType); !strings.HasPrefix(ct, echo.MIMETextHTML) {
		t.Errorf("content type = %q, want text/html", ct)
	}
	if !strings.Contains(rec.Body.String(), "<!doctype html>") {
		t.Error("expected the embedded UI to be served")
	}
}

func TestInfoHandler(t *testing.T) {
	e, st := newTestServer()
	// Two requests are observed before the JSON is rendered: the info call
	// itself and the hello call above.
	st.observe("/some/path")

	req := httptest.NewRequest(http.MethodGet, "/api/info", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var info instanceInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &info); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if info.Service != serviceName {
		t.Errorf("service = %q, want %q", info.Service, serviceName)
	}
	if info.Instance != "unit-test" {
		t.Errorf("instance = %q, want unit-test", info.Instance)
	}
	if info.Status == "" {
		t.Error("status verb should never be empty")
	}
	if info.RequestsServed == 0 {
		t.Error("requests served should be counted")
	}
	// The request middleware records the path before the handler renders it,
	// so the most recent path is the /api/info call itself.
	if info.LastPath != "/api/info" {
		t.Errorf("lastPath = %q, want /api/info", info.LastPath)
	}
	if info.LastRequestAt == nil {
		t.Error("lastRequestAt should be set once a request has been seen")
	}
	if info.Platform == "" || info.GoVersion == "" {
		t.Errorf("platform and go version should be populated: %+v", info)
	}
}

func TestHealthz(t *testing.T) {
	e, _ := newTestServer()

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if payload["status"] != "ok" {
		t.Errorf("status = %v, want ok", payload["status"])
	}
}

func TestRandomVerb(t *testing.T) {
	found := make(map[string]bool)
	for range 200 {
		v := randomVerb()
		found[v] = true
	}
	if len(found) < 10 {
		t.Errorf("expected a varied selection of verbs, got %d", len(found))
	}
	for v := range found {
		known := false
		for _, s := range spinnerVerbs {
			if s == v {
				known = true
				break
			}
		}
		if !known {
			t.Fatalf("randomVerb() returned unknown verb %q", v)
		}
	}
}

func TestRawConnect(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	host, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatalf("split: %v", err)
	}

	results := rawConnect(host, []string{port})
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].Status != "Open" {
		t.Errorf("status = %q, want Open", results[0].Status)
	}

	closed := rawConnect(host, []string{"1"})
	if closed[0].Status == "Open" {
		t.Error("connecting to a closed port should not report Open")
	}
}
