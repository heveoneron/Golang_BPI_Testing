# ==============================================================================
# Multi-stage Dockerfile for BPI Middleware Panel (Web UI & Administration)
# ==============================================================================
FROM golang:1.24-alpine AS builder

WORKDIR /build

# Install build dependencies, CA certificates, and timezone data
RUN apk add --no-cache git ca-certificates tzdata

# Cache Go modules layer
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Compile optimized static binary for Linux
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app/middleware-panel .

# ------------------------------------------------------------------------------
# Minimal Production Runtime
# ------------------------------------------------------------------------------
FROM alpine:3.20

WORKDIR /app

RUN apk add --no-cache ca-certificates tzdata curl

ENV TZ=Asia/Jakarta

# Copy binary, templates, and static assets from builder
COPY --from=builder /app/middleware-panel /app/middleware-panel
COPY --from=builder /build/templates /app/templates
COPY --from=builder /build/static /app/static

# Default port for Web Panel
EXPOSE 8091

CMD ["/app/middleware-panel"]
