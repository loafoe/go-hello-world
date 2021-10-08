FROM golang:1.17.2-alpine3.13 as builder
LABEL maintainer="andy.lo-a-foe@philips.com"
RUN apk add --no-cache git openssh gcc musl-dev
WORKDIR /go-hello-world
COPY go.mod .
COPY go.sum .

# Get dependancies - will also be cached if we won't change mod/sum
RUN go mod download

# Build
COPY . .
RUN go build .

FROM alpine:latest
RUN apk update && apk add ca-certificates && apk add postgresql-client && rm -rf /var/cache/apk/*
WORKDIR /app
COPY --from=builder /go-hello-world/go-hello-world /app

EXPOSE 8080
ENTRYPOINT ["/app/go-hello-world"]
