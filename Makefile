BINARY := aeman
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: all build frontend backend lint test fmt tidy run clean

all: build

## frontend: build the SPA into web/dist (embedded into the Go binary)
frontend:
	cd web && npm ci && npm run build

## build: build the frontend then the single self-contained binary
build: frontend backend

## backend: build only the Go binary (expects web/dist to already exist)
backend:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/aeman

## lint: run golangci-lint
lint:
	golangci-lint run

## test: run Go tests
test:
	go test ./...

## fmt: format Go code
fmt:
	golangci-lint fmt

## tidy: tidy go modules
tidy:
	go mod tidy

## run: run the server from source (frontend must be built once via `make frontend`)
run:
	go run ./cmd/aeman serve

## clean: remove build artifacts
clean:
	rm -f $(BINARY)
	rm -rf web/dist/assets web/dist/index.html
