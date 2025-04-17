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

FROM debian:latest

WORKDIR /app

RUN apt install -y tzdata

# Copy the binary from builder
COPY --from=builder /app/bin/cloudflare-gslb /app/cloudflare-gslb

# on debian, add a non-root user
RUN addgroup --system appgroup && adduser --system --ingroup appgroup appuser

USER appuser

# Set the command to run the binary
CMD ["/app/cloudflare-gslb"]
