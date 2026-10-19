.PHONY: build test check fmt vet lint install docs docs-check

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

build:
	go build -ldflags "-X main.version=$(VERSION)" -o bin/obramat ./cmd/obramat

test:
	go test ./... -count=1

fmt:
	gofmt -w .

vet:
	go vet ./...

lint:
	go run honnef.co/go/tools/cmd/staticcheck@latest ./...

check: vet lint test
	test -z "$$(gofmt -l .)"

install:
	go install -ldflags "-X main.version=$(VERSION)" ./cmd/obramat

# docs rewrites the command reference from the cobra tree; docs-check is the
# CI guard that fails when the committed pages no longer match the binary.
docs:
	go run ./cmd/docsgen

docs-check: docs
	git diff --exit-code -- docs-site/content/docs/reference
	test -z "$$(git ls-files --others --exclude-standard -- docs-site/content/docs/reference)"
