package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"log"
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
	"go.opentelemetry.io/otel/exporters/zipkin"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
	"go.opentelemetry.io/otel/trace"
)

// initTracer creates a new trace provider instance and registers it as global trace provider.
func initTracer(name, url string) func() {
	// Create Zipkin Exporter and install it as a global tracer.
	//
	// For demoing purposes, always sample. In a production application, you should
	// configure the sampler to a trace.ParentBased(trace.TraceIDRatioBased) set at the desired
	// ratio.
	exporter, err := zipkin.New(url)
	if err != nil {
		log.Fatal(err)
	}

	batcher := sdktrace.NewBatchSpanProcessor(exporter)

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(batcher),
		sdktrace.WithResource(resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceNameKey.String(name),
		)),
	)
	otel.SetTracerProvider(tp)

	return func() {
		_ = tp.Shutdown(context.Background())
	}
}

func main() {
	listenString := ":8080"

	// Echo instance
	e := echo.New()

	instanceIndex := os.Getenv("CF_INSTANCE_INDEX")
	if instanceIndex == "" {
		instanceIndex = "unknown"
	}
	reporterURL := os.Getenv("REPORTER_URL")

	shutdown := initTracer("go-hello-world", reporterURL)
	defer shutdown()

	e.Use(otelecho.Middleware("go-hello-world"))
	// Middleware
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())

	// Routes
	e.Use()
	e.GET("/", hello(instanceIndex))
	e.GET("/api/test/:host/:port", connectTester())
	e.GET("/api/dump/:base64_path", fileDumper())
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

		traceID := span.SpanContext().TraceID().String()
		fmt.Printf("traceID=%s\n", traceID)
		dump, err := httputil.DumpRequest(c.Request(), true)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, err)
		}
		return c.String(http.StatusOK, string(dump))
	}
}

func fileDumper() echo.HandlerFunc {
	return func(c echo.Context) error {
		tr := otel.GetTracerProvider().Tracer("go-hello-world")
		ctx := context.Background()
		ctx, span := tr.Start(ctx, "file_dumper", trace.WithSpanKind(trace.SpanKindServer))

		defer span.End()
		traceID := span.SpanContext().TraceID().String()
		fmt.Printf("traceID=%s\n", traceID)
		data, err := base64.StdEncoding.DecodeString(c.Param("base64_path"))
		if err != nil {
			return c.JSON(http.StatusInternalServerError, err)
		}
		filename := string(data)
		data, err = ioutil.ReadFile(filename)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, err)
		}
		_, err = c.Response().Write(data)
		return err
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
		traceID := span.SpanContext().TraceID().String()
		fmt.Printf("traceID=%s\n", traceID)
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
