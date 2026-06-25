# Stage 1: Build Go binary
FROM golang:1.26-alpine AS builder

WORKDIR /app

# Copy dependency files and download modules
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the source code
COPY . .

# Build stateless backend binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o booklet-backend .

# Stage 2: Final lightweight runner image
FROM alpine:3.19

# Add CA certificates for secure calls (if any)
RUN apk --no-cache add ca-certificates

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/booklet-backend .

# Expose backend service port
EXPOSE 8080

ENTRYPOINT ["./booklet-backend"]
