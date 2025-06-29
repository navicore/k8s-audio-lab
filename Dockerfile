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
COPY --from=builder /app/${SERVICE_NAME} ./app

# Create a script to handle conditional copy
RUN echo '#!/bin/sh' > /copy-assets.sh && \
    echo 'if [ -d "/'${SERVICE_NAME}'" ]; then' >> /copy-assets.sh && \
    echo '  cp /'${SERVICE_NAME}'/*.wav /app/ 2>/dev/null || true' >> /copy-assets.sh && \
    echo 'fi' >> /copy-assets.sh && \
    chmod +x /copy-assets.sh

# Copy service directory to root temporarily
COPY ${SERVICE_NAME}/ /${SERVICE_NAME}/

# Run the copy script and clean up
RUN /copy-assets.sh && rm -rf /${SERVICE_NAME}/ /copy-assets.sh

# Set different ports based on service
EXPOSE 8000 8001

# Use shell form to expand the variable
CMD ./app