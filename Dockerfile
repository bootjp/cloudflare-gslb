FROM golang:alpine AS builder

WORKDIR /app

# Install necessary build tools
RUN apk add --no-cache git make

# Copy go.mod and go.sum files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

RUN go build -o bin/cloudflare-gslb ./cmd/gslb/main.go

FROM debian:stable-slim

WORKDIR /app

# Install only needed packages
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    tzdata \
    && rm -rf /var/lib/apt/lists/*

# Copy the binary from builder
COPY --from=builder /app/bin/cloudflare-gslb /app/cloudflare-gslb

# Set the command to run the binary
CMD ["/app/cloudflare-gslb"]
