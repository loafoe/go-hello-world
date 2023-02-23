# go-hello-world

[![Artifact Hub](https://img.shields.io/endpoint?url=https://artifacthub.io/badge/repository/loafoe)](https://artifacthub.io/packages/search?repo=loafoe)

Simple web application in Go

## usage

* Build and push a Docker image to a registry
* Deploy it to any container orchestration platform (Kubernetes, Cloud foundry, etc)

## environment

| Variable | Description                             |
|----------|-----------------------------------------|
| PORT     | Listens to PORT instead of default 8080 |
| COLOR    | Assign color to the deployment.         |
| OTLP_ADDRESS | The address to send (gRPC) otel traces to |

## output

```
$ curl http://localhost:8080/
Hello, you've requested: /
```
