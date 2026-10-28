# ==============================================================================
# Multi-stage Dockerfile for BPI Middleware Engine (Golang API & Background Workers)
# ==============================================================================
FROM golang:alpine AS builder

WORKDIR /build

# Copy source code including vendored dependencies
COPY . .

# Compile optimized static binary using vendored modules (no external network needed)
RUN CGO_ENABLED=0 GOOS=linux go build -mod=vendor -ldflags="-s -w" -o /app/server cmd/server/main.go

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
