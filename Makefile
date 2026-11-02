VERSION ?= $(shell git describe --tags --always --dirty)
LDFLAGS = -s -w -X github.com/reorx/tinyhost/internal/version.Version=$(VERSION)
.PHONY: test build
test:
	go test ./...
build:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" ./...
