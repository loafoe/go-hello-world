package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"runtime/debug"
	"strconv"
	"time"

	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/labstack/echo-contrib/prometheus"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"go.opentelemetry.io/contrib/instrumentation/github.com/labstack/echo/otelecho"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
)

// Initializes an OTLP exporter, and configures the corresponding trace and
// metric providers.
func initProvider() (func(context.Context) error, error) {
	ctx := context.Background()

	res, err := resource.New(ctx,
		resource.WithAttributes(
			// the service name used to display traces in backends
			semconv.ServiceName("go-hello-world"),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	// If the OpenTelemetry Collector is running on a local cluster (minikube or
	// microk8s), it should be accessible through the NodePort service at the
	// `localhost:30080` endpoint. Otherwise, replace `localhost` with the
	// endpoint of your cluster. If you run the app inside k8s, then you can
	// probably connect directly to the service through dns.
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	conn, err := grpc.DialContext(ctx, os.Getenv("OTLP_ADDRESS"),
		// Note the use of insecure transport here. TLS is recommended in production.
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create gRPC connection to collector: %w", err)
	}

	// Set up a trace exporter
	traceExporter, err := otlptracegrpc.New(ctx, otlptracegrpc.WithGRPCConn(conn))
	if err != nil {
		return nil, fmt.Errorf("failed to create trace exporter: %w", err)
	}

	// Register the trace exporter with a TracerProvider, using a batch
	// span processor to aggregate spans before export.
	bsp := sdktrace.NewBatchSpanProcessor(traceExporter)
	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithResource(res),
		sdktrace.WithSpanProcessor(bsp),
	)
	otel.SetTracerProvider(tracerProvider)

	// set global propagator to tracecontext (the default is no-op).
	otel.SetTextMapPropagator(propagation.TraceContext{})

	// Shutdown will flush any remaining spans and shut down the exporter.
	return tracerProvider.Shutdown, nil
}

func main() {
	//ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	//defer cancel()
	ctx := context.Background()

	shutdown, err := initProvider()
	if err == nil {
		defer func() {
			if err := shutdown(ctx); err != nil {
				fmt.Printf("failed to shutdown TracerProvider: %v\n", err)
			}
		}()
	}

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
	tracer := otel.Tracer(fmt.Sprintf("go-hello-world-%s", instanceIndex))
	ctx = context.Background()

	e.Use(otelecho.Middleware("go-hello-world"))
	// Middleware
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())

	// Routes
	e.GET("/", hello(ctx, tracer, instanceIndex))
	e.GET("/api/test/:host/:port", connectTester())
	e.Any("/dump", requestDumper(ctx, tracer))
	e.Any("/build", infoDumper(ctx, tracer))

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

	info, ok := debug.ReadBuildInfo()
	if ok {
		fmt.Printf("build info: %s\n", info.String())
	}

	e.Logger.Fatal(e.Start(listenString))
}

type connectResult struct {
	IP     string
	Port   string
	Status string
}

// Handler
func hello(ctx context.Context, tracer trace.Tracer, instanceIndex string) echo.HandlerFunc {
	return func(c echo.Context) error {
		_, span := tracer.Start(ctx, "hello")
		defer span.End()
		return c.String(http.StatusOK, fmt.Sprintf("Hello from instance \"%s\"! You've requested: %s\n", instanceIndex, c.Request().RequestURI))
	}
}

func infoDumper(ctx context.Context, tracer trace.Tracer) echo.HandlerFunc {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return func(c echo.Context) error {
			_, span := tracer.Start(ctx, "info-dumper")
			defer span.End()
			return c.String(http.StatusInternalServerError, "build info not available")
		}
	}
	return func(c echo.Context) error {
		_, span := tracer.Start(ctx, "info-dumper")
		defer span.End()
		return c.String(http.StatusOK, info.String())
	}
}

func requestDumper(ctx context.Context, tracer trace.Tracer) echo.HandlerFunc {
	return func(c echo.Context) error {
		ctx := c.Request().Context()
		_, span := tracer.Start(ctx, "request-dumper")
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
		ctx := c.Request().Context()
		span := trace.SpanFromContext(ctx)
		defer span.End()
		host := c.Param("host")
		port := c.Param("port")
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
