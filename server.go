package main

import (
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"strconv"
	"time"

	"github.com/labstack/echo-contrib/zipkintracing"
	zipkinReporter "github.com/openzipkin/zipkin-go/reporter"
	zipkinHttpReporter "github.com/openzipkin/zipkin-go/reporter/http"

	"github.com/labstack/echo-contrib/prometheus"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/openzipkin/zipkin-go"
)

func main() {
	listenString := ":8080"

	// Echo instance
	e := echo.New()
	endpoint, err := zipkin.NewEndpoint("echo-service", "")
	if err != nil {
		e.Logger.Fatalf("error creating zipkin endpoint: %s", err.Error())
	}
	instanceIndex := os.Getenv("CF_INSTANCE_INDEX")
	if instanceIndex == "" {
		instanceIndex = "unknown"
	}
	reporterURL := os.Getenv("REPORTER_URL")
	reporter := zipkinReporter.NewNoopReporter()
	if reporterURL != "" {
		reporter = zipkinHttpReporter.NewReporter(reporterURL)
	}
	traceTags := make(map[string]string)
	traceTags["app_name"] = "go-hello-world"
	tracer, err := zipkin.NewTracer(reporter, zipkin.WithLocalEndpoint(endpoint), zipkin.WithTags(traceTags))
	//client, _ := zipkinhttp.NewClient(tracer, zipkinhttp.ClientTrace(true))

	e.Use(zipkintracing.TraceServer(tracer))
	// Middleware
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())

	// Routes
	e.GET("/", hello(instanceIndex))
	e.GET("/api/test/:host/:port", connectTester(tracer))
	e.GET("/api/dump/:base64_path", fileDumper(tracer))
	e.Any("/dump", requestDumper(tracer))

	// Metrics
	ps := echo.New()
	ps.HideBanner = true
	prom := prometheus.NewPrometheus("echo", nil)

	// Scrape metrics from main server
	e.Use(prom.HandlerFunc)
	prom.SetMetricsPath(ps)

	go func() { ps.Logger.Fatal(ps.Start(":9001")) }()

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

func requestDumper(tracer *zipkin.Tracer) echo.HandlerFunc {
	return func(c echo.Context) error {
		span := zipkintracing.StartChildSpan(c, "dump", tracer)
		defer span.Finish()

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

		traceID := span.Context().TraceID.String()
		fmt.Printf("traceID=%s\n", traceID)
		dump, err := httputil.DumpRequest(c.Request(), true)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, err)
		}
		return c.String(http.StatusOK, string(dump))
	}
}

func fileDumper(tracer *zipkin.Tracer) echo.HandlerFunc {
	return func(c echo.Context) error {
		span := zipkintracing.StartChildSpan(c, "file_dumper", tracer)
		defer span.Finish()
		traceID := span.Context().TraceID.String()
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

func connectTester(tracer *zipkin.Tracer) echo.HandlerFunc {
	return func(c echo.Context) error {
		host := c.Param("host")
		port := c.Param("port")
		span := zipkintracing.StartChildSpan(c, "raw_connect", tracer)
		defer span.Finish()
		traceID := span.Context().TraceID.String()
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
