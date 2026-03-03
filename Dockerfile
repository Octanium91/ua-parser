# Stage 1: Build
FROM golang:1.26-alpine AS builder

# Install build dependencies for CGO (required for cshared)
RUN apk add --no-cache gcc musl-dev

WORKDIR /app

# Copy go.mod and go.sum
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the source code
COPY . .

# Generate JSON resources from YAML
RUN go run ./cmd/gen-json/main.go

ARG TARGETOS
ARG TARGETARCH

# Build the REST server
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -ldflags="-s -w" -o ua-server ./cmd/server/main.go

# Build the C-shared library (requires CGO)
# Using two-step build for Linux/musl to avoid initial-exec TLS relocation issues
# -ftls-model=global-dynamic and -Wl,-Bsymbolic are required for dlopen() compatibility on Alpine
RUN if [ "$TARGETOS" = "linux" ]; then \
      CGO_ENABLED=1 GOOS=$TARGETOS GOARCH=$TARGETARCH \
      CGO_CFLAGS="-fPIC -ftls-model=global-dynamic" \
      go build -buildmode=c-archive -ldflags="-s -w" -o ua-parser.a ./cmd/cshared/main.go && \
      gcc -shared -fPIC -Wl,-Bsymbolic -o ua-parser.so \
        -Wl,--whole-archive ua-parser.a -Wl,--no-whole-archive \
        -Wl,-z,lazy -lpthread -lc && \
      rm ua-parser.a; \
    else \
      CGO_ENABLED=1 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -buildmode=c-shared -o ua-parser.so ./cmd/cshared/main.go; \
    fi

# Stage 2: Final image
FROM alpine:latest

RUN apk add --no-cache ca-certificates

WORKDIR /app

# Copy the binary and library from the builder stage
COPY --from=builder /app/ua-server .
COPY --from=builder /app/ua-parser.so .
COPY --from=builder /app/ua-parser.h .

# Expose the default port
EXPOSE 8080

# Run the server
ENTRYPOINT ["./ua-server"]
