BINARY := aeman
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

REGISTRY ?= ghcr.io/aenix-org
IMAGE_TAG ?= latest
NAMESPACE ?= aenix-aeman
RELEASE ?= aeman
CHART := charts/aeman
# Include a local (gitignored) secret override when present.
SECRET_VALUES := $(wildcard $(CHART)/values-secret.yaml)
HELM_SECRET_ARG := $(if $(SECRET_VALUES),-f $(SECRET_VALUES))

.PHONY: all build frontend backend lint test fmt tidy run clean image-push apply diff delete

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

## image-push: build+push the amd64 image and stamp its digest into the chart
image-push:
	docker buildx build . \
		--platform linux/amd64 \
		--build-arg VERSION=$(VERSION) \
		--tag $(REGISTRY)/aeman:$(IMAGE_TAG) \
		--metadata-file .build-metadata.json \
		--provenance=false --push
	@TAG=$(IMAGE_TAG)@$$(yq e '."containerimage.digest"' .build-metadata.json -o json -r 2>/dev/null || echo $(IMAGE_TAG)) \
		yq -i '.image.tag = strenv(TAG)' $(CHART)/values.yaml
	@rm -f .build-metadata.json

## apply: deploy the chart (NAMESPACE=... overrides the target namespace)
apply:
	helm upgrade -i $(RELEASE) $(CHART) -n $(NAMESPACE) --create-namespace \
		--set namespace=$(NAMESPACE) $(HELM_SECRET_ARG)

## diff: diff the chart against the cluster
diff:
	helm diff upgrade $(RELEASE) $(CHART) -n $(NAMESPACE) \
		--set namespace=$(NAMESPACE) $(HELM_SECRET_ARG)

## delete: uninstall the release
delete:
	helm uninstall $(RELEASE) -n $(NAMESPACE)
