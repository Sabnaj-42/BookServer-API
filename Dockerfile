# Stage 1: Build the CLI app
FROM golang:1.24-bookworm AS builder

WORKDIR /app

# Copy module files and download dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source code and build
COPY . .
RUN go build -o cli ./main.go

# Stage 2: Create minimal runtime image
FROM debian:bookworm-slim

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/cli .

CMD ["./cli", "start"]
