# Build stage
FROM golang:1.23.8 AS builder

WORKDIR /app

# Copy go mod files
COPY sequencer/go.mod sequencer/go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY sequencer/ ./

# Build the binary
RUN go build -o primev-sequencer .

# Runtime stage
FROM debian:bookworm-slim

# Install ca-certificates for HTTPS connections
RUN apt-get update && apt-get install -y \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Create non-root user
RUN groupadd -r primev && useradd -r -g primev primev

# Set working directory
WORKDIR /app

# Copy binary from builder stage
COPY --from=builder /app/primev-sequencer .

# Change ownership to non-root user
RUN chown primev:primev primev-sequencer

# Switch to non-root user
USER primev

# Expose ports
# EXPOSE 8545 8080 23003

# Set default command
CMD ["./primev-sequencer"]
