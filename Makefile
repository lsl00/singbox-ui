BINARY := mihomo-tui
GOBIN := $(shell go env GOPATH)/bin

.PHONY: all build test race lint check clean

all: check build

## build a static binary without CGO
build:
	CGO_ENABLED=0 go build -trimpath -buildvcs=false -ldflags="-s -w" -o $(BINARY) .

## run the tests
test:
	go test ./...

## run the tests with the race detector
race:
	go test -race ./...

## run the static analysis
lint:
	go vet ./...
	gofmt -s -l . | tee /dev/stderr | (! read)
	$(GOBIN)/staticcheck -checks=all ./...
	$(GOBIN)/deadcode ./...

## everything that has to pass before a commit
check: lint test race

## remove the build output
clean:
	rm -f $(BINARY)
