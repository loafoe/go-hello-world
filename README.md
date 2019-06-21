# go-hello-world

Simple web application in Go

# usage

* Build and push a Docker image to a registry
* Deploy it to any container orchestration platform (Kubernetes, Cloud foundry, etc)

# environment

| Variable | Description                             |
|----------|-----------------------------------------|
| PORT     | Listens to PORT instead of default 8080 |

# output

```
$ curl http://localhost:8080/foo/bar
Hello, you've requested: /foo/bar
```