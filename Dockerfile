# Build stage
FROM docker.io/golang:1.23-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git make

# Set working directory
WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build .

# Final stage
FROM gcr.io/distroless/static-debian12:nonroot

# Copy binary from builder
COPY --from=builder /app/maybe-dont /app/maybe-dont

# Copy config files
COPY --from=builder /app/config.yaml /app/config.yaml
COPY --from=builder /app/cel_rules.yaml /app/cel_rules.yaml
COPY --from=builder /app/ai_rules.yaml /app/ai_rules.yaml

# Set working directory
WORKDIR /app

# Expose port if using HTTP/SSE server
EXPOSE 8080

# Run the proxy
ENTRYPOINT ["/app/mcp-proxy"]
