FROM golang:1.21-alpine AS builder

WORKDIR /app

# Copy go mod files (we'll create them next)
COPY go.mod go.sum* ./
RUN go mod download

# Copy source code
COPY main.go .

# Build the application
RUN go build -o audio-source main.go

# Final stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /app

# Copy the binary from builder
COPY --from=builder /app/audio-source .

# Copy the audio file
COPY audio.wav audio.wav

EXPOSE 8000

CMD ["./audio-source"]