# syntax=docker/dockerfile:1

# 1. Build the SPA into web/dist.
FROM node:20-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# 2. Build the self-contained Go binary (embeds web/dist via go:embed).
FROM golang:1.26-alpine AS build
ARG VERSION=docker
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X main.version=${VERSION}" \
    -o /aeman ./cmd/aeman

# 3. Minimal runtime (static binary + CA certs, non-root).
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /aeman /aeman
EXPOSE 8765
ENTRYPOINT ["/aeman"]
CMD ["serve", "--addr=0.0.0.0:8765", "--open=false"]
