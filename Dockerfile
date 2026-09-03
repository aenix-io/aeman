# syntax=docker/dockerfile:1

# 1. Build the SPA into web/dist (arch-independent output; build on the host arch).
FROM --platform=$BUILDPLATFORM node:20-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# 2. Build the self-contained Go binary (embeds web/dist via go:embed). Built on
#    the host arch and cross-compiled to the target arch (fast, no emulation).
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS build
ARG VERSION=docker
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath \
    -ldflags "-s -w -X main.version=${VERSION}" \
    -o /aeman ./cmd/aeman
# A nonroot-owned /data so a session-file volume mounted here is writable.
RUN mkdir -p /data && chown 65532:65532 /data

# 3. Minimal runtime (static binary + CA certs, non-root).
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /aeman /aeman
COPY --from=build --chown=65532:65532 /data /data
EXPOSE 8765
ENTRYPOINT ["/aeman"]
CMD ["serve", "--addr=0.0.0.0:8765", "--open=false"]
