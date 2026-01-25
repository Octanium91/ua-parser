# Stage 1: Build
FROM golang:1.24-alpine AS builder

# Install build dependencies for CGO (required for cshared)
RUN apk add --no-cache gcc musl-dev

WORKDIR /app

# Copy go.mod and go.sum
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the source code
COPY . .

ARG TARGETOS
ARG TARGETARCH

# Build the REST server
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -ldflags="-s -w" -o ua-server ./cmd/server/main.go

# Build the C-shared library (requires CGO)
RUN CGO_ENABLED=1 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -buildmode=c-shared -o ua-parser.so ./cmd/cshared/main.go

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
