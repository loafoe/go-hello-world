package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"strconv"
	"time"

	"github.com/labstack/echo-contrib/prometheus"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"go.opentelemetry.io/contrib/instrumentation/github.com/labstack/echo/otelecho"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

func main() {
	listenString := ":8080"

	// Echo instance
	e := echo.New()

	instanceIndex := os.Getenv("CF_INSTANCE_INDEX")
	if instanceIndex == "" {
		instanceIndex = "unknown"
	}
	if color := os.Getenv("COLOR"); color != "" {
		instanceIndex = color
	}

	e.Use(otelecho.Middleware("go-hello-world"))
	// Middleware
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())

	// Routes
	e.Use()
	e.GET("/", hello(instanceIndex))
	e.GET("/api/test/:host/:port", connectTester())
	e.Any("/dump", requestDumper())

	// Metrics
	ps := echo.New()
	ps.HideBanner = true
	prom := prometheus.NewPrometheus("echo", nil)

	// Scrape metrics from main server
	e.Use(prom.HandlerFunc)
	prom.SetMetricsPath(ps)

	go func() { ps.Logger.Fatal(ps.Start(":9100")) }()

	// CF
	if port := os.Getenv("PORT"); port != "" {
		listenString = ":" + port
	}

	e.Logger.Fatal(e.Start(listenString))
}

type connectResult struct {
	IP     string
	Port   string
	Status string
}

// Handler
func hello(instanceIndex string) echo.HandlerFunc {
	return func(c echo.Context) error {
		return c.String(http.StatusOK, fmt.Sprintf("Hello from instance \"%s\"! You've requested: %s\n", instanceIndex, c.Request().RequestURI))
	}
}

func requestDumper() echo.HandlerFunc {
	return func(c echo.Context) error {
		tr := otel.GetTracerProvider().Tracer("go-hello-world")
		ctx := context.Background()
		ctx, span := tr.Start(ctx, "dump", trace.WithSpanKind(trace.SpanKindServer))

		defer span.End()

		pause := 0
		if wait := c.QueryParam("wait"); wait != "" {
			val, err := strconv.Atoi(wait)
			if err == nil {
				pause = val
			}
		}
		// Artificial wait
		if pause > 0 {
			time.Sleep(time.Duration(pause) * time.Millisecond)
		}

		dump, err := httputil.DumpRequest(c.Request(), true)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, err)
		}
		return c.String(http.StatusOK, string(dump))
	}
}

func connectTester() echo.HandlerFunc {
	return func(c echo.Context) error {
		host := c.Param("host")
		port := c.Param("port")
		tr := otel.GetTracerProvider().Tracer("go-hello-world")
		ctx := context.Background()
		ctx, span := tr.Start(ctx, "connect_tester", trace.WithSpanKind(trace.SpanKindServer))
		defer span.End()
		results := rawConnect(host, []string{port})
		return c.JSON(http.StatusOK, results)
	}
}

func rawConnect(host string, ports []string) []connectResult {
	results := make([]connectResult, len(ports))
	for i, port := range ports {
		timeout := time.Second
		conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), timeout)
		if err != nil {
			results[i] = connectResult{
				IP:     host,
				Port:   port,
				Status: fmt.Sprintf("Connection error: %s", err),
			}
		}
		if conn != nil {
			results[i] = connectResult{
				IP:     host,
				Port:   port,
				Status: fmt.Sprintf("Open"),
			}
			_ = conn.Close()
		}
	}
	return results
}
