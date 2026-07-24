GO ?= go
BINARY ?= yaa

VERSION ?= 0.1.0
GIT_COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_TIME ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -X github.com/imshuai/yaa/internal/api.Version=$(VERSION) \
           -X github.com/imshuai/yaa/internal/api.GitCommit=$(GIT_COMMIT) \
           -X github.com/imshuai/yaa/internal/api.BuildTime=$(BUILD_TIME)

.PHONY: build test fmt

build:
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/yaa

test:
	$(GO) test ./...

fmt:
	$(GO) fmt ./...
