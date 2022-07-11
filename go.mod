module github.com/philips-software/go-hello-world

go 1.12

require (
	github.com/labstack/echo-contrib v0.12.0
	github.com/labstack/echo/v4 v4.7.2
	go.opentelemetry.io/contrib/instrumentation/github.com/labstack/echo/otelecho v0.32.0
	go.opentelemetry.io/otel v1.8.0
	go.opentelemetry.io/otel/exporters/zipkin v1.7.0
	go.opentelemetry.io/otel/sdk v1.7.0
	go.opentelemetry.io/otel/trace v1.8.0
)
