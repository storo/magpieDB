# Build stage
FROM golang:1.23-alpine AS builder

WORKDIR /build

# Install build dependencies
RUN apk add --no-cache git make

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -ldflags="-w -s" -o magpie ./cmd/magpie

# Final stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /app

# Copy binary from builder
COPY --from=builder /build/magpie /usr/local/bin/magpie

# Create data directory
RUN mkdir -p /data

# Set working directory for database files
WORKDIR /data

# Run as non-root user
RUN addgroup -S magpie && adduser -S magpie -G magpie
RUN chown -R magpie:magpie /data
USER magpie

ENTRYPOINT ["magpie"]
CMD ["--help"]
