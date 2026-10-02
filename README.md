# go-hello-world

[![Artifact Hub](https://img.shields.io/endpoint?url=https://artifacthub.io/badge/repository/loafoe)](https://artifacthub.io/packages/search?repo=loafoe)
[![Go Report Card](https://goreportcard.com/badge/github.com/loafoe/go-hello-world)](https://goreportcard.com/report/github.com/loafoe/go-hello-world)

Small, deliberately boring microservice used to smoke-test clusters: it
greets you, tells you which instance answered, and exposes its own build
metadata, request dumps, TCP connectivity checks, Prometheus metrics and
OpenTelemetry traces.

The landing page is a self-contained web UI compiled into the binary with
`go:embed` — no CDN, no bundler, no client-side framework.

## usage

* Build and push to a registry
* Deploy it to any container orchestration platform (Kubernetes, Cloud Foundry, etc)

## endpoints

| Method     | Path                    | Description                                                        |
|------------|-------------------------|--------------------------------------------------------------------|
| `GET`      | `/`                     | Web UI for browsers, plain-text greeting for `curl` and probes       |
| `GET`      | `/api/info`             | Live instance metadata as JSON (uptime, version, memory, counters)   |
| `GET`      | `/healthz`              | Liveness/readiness probe                                           |
| `GET`      | `/api/test/:host/:port` | Test a TCP connection and report the result                         |
| `ANY`      | `/dump`                 | Dump the incoming request (supports `?wait=<ms>` to add latency)     |
| `ANY`      | `/build`                | Go build information                                               |
| `GET`      | `:9100/metrics`         | Prometheus metrics                                                 |

## local development

```bash
go run .
# open http://localhost:8080 in a browser, or:
curl http://localhost:8080/

COLOR=emerald go run .   # name the instance
```

Run the tests with:

```bash
go test ./...
```

## environment

| Variable      | Description                                      | Default |
|---------------|--------------------------------------------------|---------|
| `PORT`        | Listen on `PORT` instead of 8080                 | `8080`  |
| `COLOR`       | Assign a colour/name to the deployment           | —       |
| `CF_INSTANCE_INDEX` | Cloud Foundry instance index             | —       |
| `OTLP_ADDRESS` | `host:port` of the OTLP/gRPC collector to send traces to | — |

If `OTLP_ADDRESS` is unset or unreachable the service still starts, and says
so in its logs. Metrics are always served on `:9100`.

## kustomize

Kustomize output should be run through `envsubst` with the following variables set

| Variable | Description |
|----------|-------------|
| `namespace` | The namespace to create all resources in |
| `ingress_host` | The ingress hostname to use |
| `ingress_fqdn` | The ingress FQDN. This is appended to the ingress hostname |

## output

```
$ curl http://localhost:8080/
Hello from instance "colorless"! You've requested: /
```

## helm

```bash
helm install hello oci://ghcr.io/loafoe/helm-charts/go-hello-world --version 0.17.0
```
