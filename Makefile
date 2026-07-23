GO ?= go
BINARY ?= yaa

.PHONY: build test fmt

build:
	$(GO) build -o $(BINARY) ./cmd/yaa

test:
	$(GO) test ./...

fmt:
	$(GO) fmt ./...
