# Stage 1: Build
# Runs on the BUILD machine's own architecture and cross-compiles to
# TARGETARCH below (pure Go, CGO off). Without --platform the arm64 image
# would run the whole Go toolchain under QEMU emulation, which is several
# times slower and known-flaky (golang/go#68976).
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS builder

WORKDIR /app

# Copy go.mod and go.sum
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the source code
COPY . .

ARG TARGETOS
ARG TARGETARCH

# Build the REST server (build the package, not a single file, so any future
# multi-file addition to cmd/server keeps compiling)
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -ldflags="-s -w" -o ua-server ./cmd/server

# Stage 2: Final image
FROM alpine:latest

RUN apk add --no-cache ca-certificates

WORKDIR /app

# Copy the server binary from the builder stage
COPY --from=builder /app/ua-server .

# Expose the default port
EXPOSE 8080

# Liveness/readiness probe hitting the built-in health endpoint (busybox wget).
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget -qO- http://localhost:8080/health || exit 1

# Run the server
ENTRYPOINT ["./ua-server"]
