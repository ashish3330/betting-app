# ---------------------------------------------------------------
# Stage 1: Build the Go binary
# ---------------------------------------------------------------
FROM golang:1.24-alpine AS builder

RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /app

# Cache dependency downloads
COPY go.mod go.sum ./
RUN go mod download && go mod verify

# Copy source and build
COPY . .

ARG VERSION=dev
ARG COMMIT=unknown

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" \
    -o /gateway ./cmd/gateway

# ---------------------------------------------------------------
# Stage 2: Minimal production image
# ---------------------------------------------------------------
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata wget \
    && addgroup -S lotus \
    && adduser -S -G lotus -h /home/lotus lotus

# Copy the compiled binary
COPY --from=builder /gateway /usr/local/bin/gateway

# Copy migrations so the container can run them at startup if needed
COPY --from=builder /app/migrations /migrations

# Drop to non-root user
USER lotus

EXPOSE 8080 8081

HEALTHCHECK --interval=10s --timeout=5s --start-period=15s --retries=3 \
    CMD ["wget", "--spider", "-q", "http://localhost:8080/healthz"]

# Use exec form for proper signal handling (SIGTERM)
ENTRYPOINT ["gateway"]
