# K8s Audio Lab

This repository contains test applications for monitoring audio latency in Kubernetes environments. It includes two services that can be deployed to test and monitor audio streaming performance.

## Components

### Audio Source Service
- Streams audio data in chunks from a WAV file
- Provides a web interface for monitoring
- Runs on port 8000

### Audio Relay Service
- Receives audio from the source service
- Adds configurable latency (0-15 seconds)
- Provides buffering and delay simulation
- Runs on port 8001

## Quick Start

### Local Development

1. **Build Docker images locally:**
   ```bash
   docker build -f Dockerfile --build-arg SERVICE_NAME=audio-source -t audio-source .
   docker build -f Dockerfile --build-arg SERVICE_NAME=audio-relay -t audio-relay .
   ```

2. **Run with Docker Compose:**
   ```bash
   docker run -p 8000:8000 audio-source
   docker run -p 8001:8001 -e AUDIO_SOURCE_URL=http://host.docker.internal:8000 audio-relay
   ```

### Kubernetes Deployment

**Using Helm (recommended):**

```bash
# Add the Helm repository
helm repo add navicore https://www.navicore.tech/charts
helm repo update

# Install with default values
helm install audio-lab navicore/audio-lab

# Or install with your custom domain
helm install audio-lab navicore/audio-lab --set global.domain=yourdomain.com
```

### AWS EKS Deployment

See [eks/README.md](eks/README.md) for detailed instructions on deploying to AWS EKS with TLS.

## GitHub Actions CI/CD

The repository includes GitHub Actions workflows that:
1. Build Docker images for both services
2. Push images to Docker Hub
3. Tag images with branch names and commit SHAs

### Setup GitHub Secrets

Add these secrets to your GitHub repository:
- `DOCKER_HUB_USERNAME`: Your Docker Hub username
- `DOCKER_HUB_TOKEN`: Your Docker Hub access token

## Helm Chart

The Helm chart supports:
- Configurable resource limits
- Automatic ingress creation with `global.domain`
- TLS support
- Horizontal Pod Autoscaling
- Security contexts
- Service accounts

The chart is available from: https://www.navicore.tech/charts/

## Architecture

```
┌─────────────┐         ┌─────────────┐
│Audio Source │ ──SSE──▶│ Audio Relay │
│  Port 8000  │         │  Port 8001  │
└─────────────┘         └─────────────┘
       │                       │
       └───────────┬───────────┘
                   │
                   ▼
              [Web Clients]
```

## Configuration

### Audio Source
- WAV file: `/app/audio.wav`
- Chunk duration: 100ms
- Supports multiple concurrent clients

### Audio Relay
- Configurable delay: 0-15 seconds
- Buffer size: 20 seconds
- Environment variable: `AUDIO_SOURCE_URL`

## Monitoring

Both services expose `/status` endpoints for health checks and monitoring.

## License

MIT License