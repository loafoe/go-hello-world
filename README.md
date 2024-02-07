# go-hello-world

[![Artifact Hub](https://img.shields.io/endpoint?url=https://artifacthub.io/badge/repository/loafoe)](https://artifacthub.io/packages/search?repo=loafoe)

Simple web application in Go, includes OpenTelemetry instrumentation.

## usage

* Build and push to a registry
* Deploy it to any container orchestration platform (Kubernetes, Cloud foundry, etc)

## environment

| Variable | Description                             |
|----------|-----------------------------------------|
| PORT     | Listens to PORT instead of default 8080 |
| COLOR    | Assign color to the deployment.         |
| OTLP_ADDRESS | The address to send (gRPC) otel traces to |

## kustomize

Kustomize output should be run through `envsubst` with the following variables set

| Variable | Description |
|----------|-------------|
| namespace| The namespace to creata all resources in |
| ingress_host | The ingress hostname to use |
| ingress_fqdn | The ingress FQDN. This is appended to the ingress hostname |


## output

```
$ curl http://localhost:8080/
Hello, you've requested: /
```
