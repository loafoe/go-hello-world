#!/bin/sh

set -x

GIT_COMMIT=$(git rev-parse --short HEAD)

cd $GOPATH
wget -O - -q https://install.goreleaser.com/github.com/golangci/golangci-lint.sh | sh -s v1.16.0

cd /src
go mod download
go get -u github.com/jstemmer/go-junit-report
go get -u github.com/t-yuki/gocover-cobertura
go get github.com/axw/gocov/gocov
go get github.com/AlekSi/gocov-xml
go get -u gopkg.in/matm/v1/gocov-html

rm -rf build/ && mkdir -p build
go build -ldflags "-X main.commit=${GIT_COMMIT}" -o build/go-hello-world .
go test -coverprofile build/coverage.out -covermode count -v ./... 2>&1  > build/test-result.txt
gocov convert build/coverage.out > build/coverage.json
gocov-xml < build/coverage.json > build/coverage.xml
mkdir -p build/coverage
gocov-html < build/coverage.json > build/coverage/index.html
go-junit-report < build/test-result.txt > build/TEST-report.xml
gocover-cobertura < build/coverage.out > build/coverage-cobertura.xml
golangci-lint run
