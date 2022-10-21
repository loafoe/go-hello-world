# go-hello-world

[![Artifact Hub](https://img.shields.io/endpoint?url=https://artifacthub.io/badge/repository/loafoe)](https://artifacthub.io/packages/search?repo=loafoe)

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

# kubernetes deployment

* Use the [deployment.yml](deployment.yml) example

```
$ kubectl apply -f deployment.yml
```
* Expose the deployment through a service called `hello-world`

```
$ kubectl expose deployment go-hello-world --type=NodePort --name=hello-world
```

