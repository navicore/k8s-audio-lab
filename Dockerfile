# Multi-stage build for both audio services
FROM golang:1.21-alpine AS builder

ARG SERVICE_NAME

WORKDIR /app

# Copy appropriate service files
COPY ${SERVICE_NAME}/go.mod ${SERVICE_NAME}/go.sum* ./
RUN go mod download

# Copy source code
COPY ${SERVICE_NAME}/main.go .

# Build the application
RUN go build -o ${SERVICE_NAME} main.go

# Final stage
FROM alpine:latest

ARG SERVICE_NAME

RUN apk --no-cache add ca-certificates

WORKDIR /app

# Copy the binary from builder
COPY --from=builder /app/${SERVICE_NAME} .

# Copy audio files if this is the audio-source service
RUN if [ "$SERVICE_NAME" = "audio-source" ]; then \
    echo "Audio source service detected"; \
    fi

# Copy audio files for audio-source
COPY ${SERVICE_NAME}/*.wav* ./

# Set different ports based on service
EXPOSE 8000 8001

# Use the service name as the command
CMD ["./${SERVICE_NAME}"]