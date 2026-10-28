# ==============================================================================
# Multi-stage Dockerfile for BPI Middleware Panel (Web UI & Administration)
# ==============================================================================
FROM golang:alpine AS builder

WORKDIR /build

# Copy source code including vendored dependencies
COPY . .

# Compile optimized static binary using vendored modules (no external network needed)
RUN CGO_ENABLED=0 GOOS=linux go build -mod=vendor -ldflags="-s -w" -o /app/middleware-panel .

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
