# ==============================================================================
# Multi-stage Dockerfile for BPI Middleware Engine (Golang API & Background Workers)
# ==============================================================================
FROM golang:1.24-alpine AS builder

WORKDIR /build

# Install build dependencies, CA certificates, and timezone data
RUN apk add --no-cache git ca-certificates tzdata

# Cache Go modules layer
COPY go.mod go.sum ./
RUN go mod download

# Copy application source code
COPY . .

# Compile optimized static binary for Linux
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app/server cmd/server/main.go

# ------------------------------------------------------------------------------
# Minimal Production Runtime
# ------------------------------------------------------------------------------
FROM alpine:3.20

WORKDIR /app

RUN apk add --no-cache ca-certificates tzdata curl

ENV TZ=Asia/Jakarta

# Copy binary from builder
COPY --from=builder /app/server /app/server
COPY --from=builder /build/database /app/database

# Default port for Middleware Engine
EXPOSE 8085

CMD ["/app/server"]
