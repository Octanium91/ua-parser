# Stage 1: Build
FROM golang:1.26-alpine AS builder

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

# Stage 2: Final image
FROM alpine:latest

RUN apk add --no-cache ca-certificates

WORKDIR /app

# Copy the server binary from the builder stage
COPY --from=builder /app/ua-server .

# Expose the default port
EXPOSE 8080

# Run the server
ENTRYPOINT ["./ua-server"]
