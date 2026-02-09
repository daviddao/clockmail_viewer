# cmv Makefile
#
# Build the clockmail viewer TUI

.PHONY: build install clean test lint

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.Version=$(VERSION)

build:
	go build -ldflags "$(LDFLAGS)" -o cmv ./cmd/cmv

install:
	go install -ldflags "$(LDFLAGS)" ./cmd/cmv

clean:
	rm -f cmv
	go clean

test:
	go test ./...

lint:
	golangci-lint run ./...
