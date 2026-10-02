// Command go-hello-world is a tiny, dependency-light HTTP service used to
// smoke-test deployments, ingress, tracing and metrics plumbing.
//
// It serves a self-contained web UI (embedded in the binary, no external
// assets), a JSON introspection endpoint, a handful of diagnostic endpoints,
// Prometheus metrics and OpenTelemetry traces.
package main

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"os/signal"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/labstack/echo-contrib/prometheus"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	slogecho "github.com/samber/slog-echo"
	"go.opentelemetry.io/contrib/instrumentation/github.com/labstack/echo/otelecho"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"
	_ "go.uber.org/automaxprocs"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// The UI is compiled into the binary: a single artefact, no CDN, no sidecar.
//
//go:embed ui/index.html
var ui []byte

const (
	serviceName    = "go-hello-world"
	defaultAddress = ":8080"
	metricsAddress = ":9100"
)

// Overridable at link time, e.g.
//
//	-X main.version=v1.2.3 -X main.revision=deadbeef -X main.dirty=false
var (
	version  = "dev"
	revision = ""
	dirty    = "false"
)

var spinnerVerbs = []string{
	"Accomplishing", "Actioning", "Actualizing", "Architecting", "Baking",
	"Beaming", "Beboppin'", "Befuddling", "Billowing", "Blanching",
	"Bloviating", "Boogieing", "Boondoggling", "Booping", "Bootstrapping",
	"Brewing", "Bunning", "Burrowing", "Calculating", "Canoodling",
	"Caramelizing", "Cascading", "Catapulting", "Cerebrating", "Channeling",
	"Channelling", "Choreographing", "Churning", "Clauding", "Coalescing",
	"Cogitating", "Combobulating", "Composing", "Computing", "Concocting",
	"Considering", "Contemplating", "Cooking", "Crafting", "Creating",
	"Crunching", "Crystallizing", "Cultivating", "Deciphering", "Deliberating",
	"Determining", "Dilly-dallying", "Discombobulating", "Doing", "Doodling",
	"Drizzling", "Ebbing", "Effecting", "Elucidating", "Embellishing",
	"Enchanting", "Envisioning", "Evaporating", "Fermenting", "Fiddle-faddling",
	"Finagling", "Flambéing", "Flibbertigibbeting", "Flowing", "Flummoxing",
	"Fluttering", "Forging", "Forming", "Frolicking", "Frosting",
	"Gallivanting", "Galloping", "Garnishing", "Generating", "Gesticulating",
	"Germinating", "Gitifying", "Grooving", "Gusting", "Harmonizing",
	"Hashing", "Hatching", "Herding", "Honking", "Hullaballooing",
	"Hyperspacing", "Ideating", "Imagining", "Improvising", "Incubating",
	"Inferring", "Infusing", "Ionizing", "Jitterbugging", "Julienning",
	"Kneading", "Leavening", "Levitating", "Lollygagging", "Manifesting",
	"Marinating", "Meandering", "Metamorphosing", "Misting", "Moonwalking",
	"Moseying", "Mulling", "Mustering", "Musing", "Nebulizing",
	"Nesting", "Newspapering", "Noodling", "Nucleating", "Orbiting",
	"Orchestrating", "Osmosing", "Perambulating", "Percolating", "Perusing",
	"Philosophising", "Photosynthesizing", "Pollinating", "Pondering", "Pontificating",
	"Pouncing", "Precipitating", "Prestidigitating", "Processing", "Proofing",
	"Propagating", "Puttering", "Puzzling", "Quantumizing", "Razzle-dazzling",
	"Razzmatazzing", "Recombobulating", "Reticulating", "Roosting", "Ruminating",
	"Sautéing", "Scampering", "Schlepping", "Scurrying", "Seasoning",
	"Shenaniganing", "Shimmying", "Simmering", "Skedaddling", "Sketching",
	"Slithering", "Smooshing", "Sock-hopping", "Spelunking", "Spinning",
	"Sprouting", "Stewing", "Sublimating", "Swirling", "Swooping",
	"Symbioting", "Synthesizing", "Tempering", "Thinking", "Thundering",
	"Tinkering", "Tomfoolering", "Topsy-turvying", "Transfiguring", "Transmuting",
	"Twisting", "Undulating", "Unfurling", "Unravelling", "Vibing",
	"Waddling", "Wandering", "Warping", "Whatchamacalliting", "Whirlpooling",
	"Whirring", "Whisking", "Wibbling", "Working", "Wrangling",
	"Zesting", "Zigzagging",
}

// buildInfo is the immutable description of the running binary.
type buildInfo struct {
	Version    string `json:"version"`
	Revision   string `json:"revision"`
	Dirty      bool   `json:"dirty"`
	CommitTime string `json:"commitTime,omitempty"`
	GoVersion  string `json:"goVersion"`
	OS         string `json:"os"`
	Arch       string `json:"arch"`
	Raw        string `json:"-"`
}

// readBuildInfo assembles the build description from link-time variables,
// falling back to the VCS stamps the Go toolchain embeds automatically.
func readBuildInfo() buildInfo {
	bi := buildInfo{
		Version:   version,
		Revision:  revision,
		GoVersion: runtime.Version(),
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
	}

	if d, err := strconv.ParseBool(dirty); err == nil {
		bi.Dirty = d
	}

	info, ok := debug.ReadBuildInfo()
	if !ok {
		return bi
	}
	bi.Raw = info.String()
	bi.GoVersion = info.GoVersion

	if bi.Revision == "" {
		bi.Revision = setting(info, "vcs.revision")
	}
	if bi.CommitTime == "" {
		bi.CommitTime = setting(info, "vcs.time")
	}
	if m, err := strconv.ParseBool(setting(info, "vcs.modified")); err == nil && m {
		bi.Dirty = true
	}
	if bi.Version == "dev" && info.Main.Version != "" && info.Main.Version != "(devel)" {
		bi.Version = info.Main.Version
	}

	return bi
}

func setting(info *debug.BuildInfo, key string) string {
	for _, s := range info.Settings {
		if s.Key == key {
			return s.Value
		}
	}
	return ""
}

// state holds the mutable, request-scoped counters rendered by the UI.
type state struct {
	startedAt time.Time
	requests  atomic.Uint64
	lastPath  atomic.Value // string
	lastSeen  atomic.Int64 // unix nanoseconds
}

func newState() *state {
	s := &state{startedAt: time.Now()}
	s.lastPath.Store("")
	s.lastSeen.Store(time.Time{}.UnixNano())
	return s
}

func (s *state) observe(path string) {
	s.requests.Add(1)
	s.lastPath.Store(path)
	s.lastSeen.Store(time.Now().UnixNano())
}

func (s *state) lastRequest() time.Time {
	ns := s.lastSeen.Load()
	if ns == 0 {
		return time.Time{}
	}
	return time.Unix(0, ns)
}

// instanceInfo is the payload behind GET /api/info.
type instanceInfo struct {
	Service          string     `json:"service"`
	Version          string     `json:"version"`
	Revision         string     `json:"revision"`
	Dirty            bool       `json:"dirty"`
	Instance         string     `json:"instance"`
	Status           string     `json:"status"`
	Hostname         string     `json:"hostname"`
	PID              int        `json:"pid"`
	GoVersion        string     `json:"goVersion"`
	Platform         string     `json:"platform"`
	StartedAt        time.Time  `json:"startedAt"`
	Uptime           string     `json:"uptime"`
	UptimeSeconds    float64    `json:"uptimeSeconds"`
	Goroutines       int        `json:"goroutines"`
	MemoryAllocBytes uint64     `json:"memoryAllocBytes"`
	RequestsServed   uint64     `json:"requestsServed"`
	LastPath         string     `json:"lastPath"`
	LastRequestAt    *time.Time `json:"lastRequestAt,omitempty"`
	Tracing          bool       `json:"tracing"`
	OTLPAddress      string     `json:"otelAddress,omitempty"`
}

func humanDuration(d time.Duration) string {
	if d < time.Second {
		return "0s"
	}
	return d.Round(time.Second).String()
}

func randomVerb() string {
	return spinnerVerbs[rand.IntN(len(spinnerVerbs))]
}

// initProvider configures the OTLP trace exporter. The returned function
// flushes and shuts the provider down.
func initProvider(ctx context.Context, bi buildInfo) (func(context.Context) error, error) {
	address := os.Getenv("OTLP_ADDRESS")
	if address == "" {
		return nil, errors.New("OTLP_ADDRESS is not set")
	}

	res, err := resource.Merge(resource.Default(), resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName(serviceName),
		semconv.ServiceVersion(bi.Version),
	))
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	// Fail fast when the collector is not reachable: there is no point in
	// starting a service that can never report its own traces. If the
	// OpenTelemetry Collector runs in the same cluster (k3s, minikube,
	// microk8s) it is usually reachable over DNS.
	dialCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()

	var dialer net.Dialer
	conn, err := dialer.DialContext(dialCtx, "tcp", address)
	if err != nil {
		return nil, fmt.Errorf("failed to reach collector at %s: %w", address, err)
	}
	_ = conn.Close()

	// Note the use of insecure transport here. TLS is recommended in production.
	client, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("failed to create gRPC client for collector: %w", err)
	}

	traceExporter, err := otlptracegrpc.New(ctx, otlptracegrpc.WithGRPCConn(client))
	if err != nil {
		return nil, fmt.Errorf("failed to create trace exporter: %w", err)
	}

	// Batch span processor aggregates spans before exporting them.
	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.AlwaysSample())),
		sdktrace.WithResource(res),
		sdktrace.WithSpanProcessor(sdktrace.NewBatchSpanProcessor(traceExporter)),
	)
	otel.SetTracerProvider(tracerProvider)

	// Set the global propagator to tracecontext (the default is no-op).
	otel.SetTextMapPropagator(propagation.TraceContext{})

	// Shutdown will flush any remaining spans and shut down the exporter.
	return tracerProvider.Shutdown, nil
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	bi := readBuildInfo()

	shutdownProvider, err := initProvider(ctx, bi)
	if err != nil {
		logger.Error("tracing disabled", "error", err)
	} else {
		defer func() {
			if err := shutdownProvider(context.Background()); err != nil {
				logger.Error("failed to shutdown TracerProvider", "error", err)
			}
		}()
	}

	listen := defaultAddress
	// Cloud Foundry assigns the port via $PORT.
	if port := os.Getenv("PORT"); port != "" {
		listen = ":" + port
	}

	instance := os.Getenv("CF_INSTANCE_INDEX")
	if instance == "" {
		instance = "unknown"
	}
	if color := os.Getenv("COLOR"); color != "" {
		instance = color
	}

	st := newState()
	tracer := otel.Tracer(fmt.Sprintf("%s-%s", serviceName, instance))

	e := echo.New()
	e.HideBanner = true
	e.HidePort = true

	e.Use(otelecho.Middleware(serviceName))
	e.Use(slogecho.New(logger))
	e.Use(middleware.Recover())
	e.Use(middleware.RequestID())
	// The UI is entirely self-contained: nothing is ever loaded from the
	// network, so lock that in with a restrictive CSP.
	e.Use(middleware.SecureWithConfig(middleware.SecureConfig{
		XSSProtection:         "0",
		ContentSecurityPolicy: "default-src 'none'; img-src 'self' data:; style-src 'unsafe-inline'; script-src 'unsafe-inline'; connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'",
	}))
	e.Use(countRequests(st))

	prom := prometheus.NewPrometheus(serviceName, nil)
	e.Use(prom.HandlerFunc)

	// The UI and the diagnostic endpoints.
	e.GET("/", hello(logger, tracer, instance))
	e.GET("/api/info", infoHandler(tracer, st, instance, bi))
	e.GET("/healthz", healthz(st))
	e.GET("/api/test/:host/:port", connectTester(logger, tracer))
	e.Any("/dump", requestDumper(logger, tracer))
	e.Any("/build", infoDumper(logger, tracer, bi))

	// Metrics are served on their own, unauthenticated port.
	metrics := echo.New()
	metrics.HideBanner = true
	metrics.HidePort = true
	prom.SetMetricsPath(metrics)
	go func() {
		if err := metrics.Start(metricsAddress); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("metrics server stopped", "error", err)
		}
	}()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := metrics.Shutdown(shutdownCtx); err != nil {
			logger.Error("failed to shutdown metrics server", "error", err)
		}
	}()

	logger.Info("starting",
		"service", serviceName,
		"version", bi.Version,
		"revision", bi.Revision,
		"dirty", bi.Dirty,
		"instance", instance,
		"address", listen,
		"metrics", metricsAddress,
		"go", bi.GoVersion,
		"cpus", runtime.NumCPU(),
	)

	serveErr := make(chan error, 1)
	go func() { serveErr <- e.Start(listen) }()

	select {
	case err := <-serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server stopped", "error", err)
			os.Exit(1)
		}
	case <-ctx.Done():
		logger.Info("shutdown signal received, draining")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := e.Shutdown(shutdownCtx); err != nil {
			logger.Error("graceful shutdown failed", "error", err)
		}
	}
}

// countRequests keeps the cheap counters rendered by the UI.
func countRequests(st *state) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			st.observe(c.Request().URL.Path)
			return next(c)
		}
	}
}

type connectResult struct {
	IP     string
	Port   string
	Status string
}

// hello serves the embedded UI to browsers and a plain-text greeting to
// everything else (curl, probes, scripts).
func hello(logger *slog.Logger, tracer trace.Tracer, instance string) echo.HandlerFunc {
	return func(c echo.Context) error {
		_, span := tracer.Start(c.Request().Context(), "hello")
		defer span.End()

		sc := span.SpanContext()
		logger.Info("hello",
			"instance", instance,
			"path", c.Request().RequestURI,
			"trace_id", sc.TraceID(),
			"span_id", sc.SpanID(),
		)

		if !wantsHTML(c.Request()) {
			c.Response().Header().Set(echo.HeaderContentType, echo.MIMETextPlainCharsetUTF8)
			return c.String(http.StatusOK,
				fmt.Sprintf("Hello from instance %q! You've requested: %s\n", instance, c.Request().RequestURI))
		}

		c.Response().Header().Set(echo.HeaderContentType, echo.MIMETextHTMLCharsetUTF8)
		c.Response().Header().Set("Cache-Control", "no-cache")
		return c.HTMLBlob(http.StatusOK, ui)
	}
}

// infoHandler backs the live dashboard in the UI.
func infoHandler(tracer trace.Tracer, st *state, instance string, bi buildInfo) echo.HandlerFunc {
	return func(c echo.Context) error {
		_, span := tracer.Start(c.Request().Context(), "instance-info")
		defer span.End()
		span.SetAttributes(attribute.String("instance", instance))

		var mem runtime.MemStats
		runtime.ReadMemStats(&mem)

		hostname, _ := os.Hostname()

		info := instanceInfo{
			Service:          serviceName,
			Version:          bi.Version,
			Revision:         bi.Revision,
			Dirty:            bi.Dirty,
			Instance:         instance,
			Status:           randomVerb(),
			Hostname:         hostname,
			PID:              os.Getpid(),
			GoVersion:        bi.GoVersion,
			Platform:         bi.OS + "/" + bi.Arch,
			StartedAt:        st.startedAt.UTC(),
			Uptime:           humanDuration(time.Since(st.startedAt)),
			UptimeSeconds:    time.Since(st.startedAt).Seconds(),
			Goroutines:       runtime.NumGoroutine(),
			MemoryAllocBytes: mem.HeapAlloc,
			RequestsServed:   st.requests.Load(),
			LastPath:         fmt.Sprintf("%v", st.lastPath.Load()),
			Tracing:          span.SpanContext().IsValid(),
		}
		if last := st.lastRequest(); !last.IsZero() {
			t := last.UTC()
			info.LastRequestAt = &t
		}
		if addr := os.Getenv("OTLP_ADDRESS"); addr != "" {
			info.OTLPAddress = addr
		}

		return c.JSON(http.StatusOK, info)
	}
}

func healthz(st *state) echo.HandlerFunc {
	return func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]any{
			"status":  "ok",
			"uptime":  humanDuration(time.Since(st.startedAt)),
			"version": version,
		})
	}
}

func infoDumper(logger *slog.Logger, tracer trace.Tracer, bi buildInfo) echo.HandlerFunc {
	if bi.Raw == "" {
		return func(c echo.Context) error {
			_, span := tracer.Start(c.Request().Context(), "info-dumper")
			defer span.End()
			return c.String(http.StatusInternalServerError, "build info not available")
		}
	}
	return func(c echo.Context) error {
		_, span := tracer.Start(c.Request().Context(), "info-dumper")
		defer span.End()
		sc := span.SpanContext()
		logger.Info("build info", "trace_id", sc.TraceID(), "span_id", sc.SpanID())
		return c.String(http.StatusOK, bi.Raw)
	}
}

func requestDumper(logger *slog.Logger, tracer trace.Tracer) echo.HandlerFunc {
	return func(c echo.Context) error {
		ctx, span := tracer.Start(c.Request().Context(), "request-dumper")
		defer span.End()

		pause := 0
		if wait := c.QueryParam("wait"); wait != "" {
			if val, err := strconv.Atoi(wait); err == nil {
				pause = val
			}
		}
		if pause > 0 {
			select {
			case <-time.After(time.Duration(pause) * time.Millisecond):
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		dump, err := httputil.DumpRequest(c.Request(), true)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, err)
		}
		sc := span.SpanContext()
		logger.Info("request", "trace_id", sc.TraceID(), "span_id", sc.SpanID())
		return c.String(http.StatusOK, string(dump))
	}
}

func connectTester(logger *slog.Logger, tracer trace.Tracer) echo.HandlerFunc {
	return func(c echo.Context) error {
		_, span := tracer.Start(c.Request().Context(), "connect-tester")
		defer span.End()

		host := c.Param("host")
		port := c.Param("port")
		results := rawConnect(host, []string{port})

		span.SetStatus(codes.Ok, "got connect test")
		if len(results) > 0 {
			span.SetAttributes(
				attribute.String("target", fmt.Sprintf("%s:%s", host, port)),
				attribute.String("result", results[0].Status),
			)
			sc := span.SpanContext()
			logger.Info("connection tested",
				"status", results[0].Status,
				"trace_id", sc.TraceID(),
				"span_id", sc.SpanID(),
			)
		}
		return c.JSON(http.StatusOK, results)
	}
}

func rawConnect(host string, ports []string) []connectResult {
	results := make([]connectResult, len(ports))
	for i, port := range ports {
		conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), time.Second)
		if err != nil {
			results[i] = connectResult{
				IP:     host,
				Port:   port,
				Status: fmt.Sprintf("Connection error: %s", err),
			}
			continue
		}
		results[i] = connectResult{
			IP:     host,
			Port:   port,
			Status: "Open",
		}
		_ = conn.Close()
	}
	return results
}

// wantsHTML reports whether the caller is (likely) a browser.
func wantsHTML(r *http.Request) bool {
	return strings.Contains(r.Header.Get(echo.HeaderAccept), echo.MIMETextHTML)
}
