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

# 3. Runtime: a small image carrying the static Go binary plus `git`, which
#    maintenance shells out to for repacking the store (go-git's own
#    RepackObjects overflows the stack on a real history). ca-certificates for
#    HTTPS to the forge and tzdata for the board's named timezones; run as a
#    non-root user, uid matching the distroless `nonroot` it replaced so an
#    existing /data volume stays writable.
FROM alpine:3.21
RUN apk add --no-cache git ca-certificates tzdata \
 && addgroup -g 65532 nonroot \
 && adduser -D -H -u 65532 -G nonroot nonroot
COPY --from=build /aeman /aeman
COPY --from=build --chown=65532:65532 /data /data
USER 65532:65532
EXPOSE 8765
ENTRYPOINT ["/aeman"]
CMD ["serve", "--addr=0.0.0.0:8765", "--open=false"]
