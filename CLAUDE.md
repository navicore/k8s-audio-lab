# K8s Audio Lab - Development Notes

## Current Setup
- **Cluster**: EKS 1.31 with 2-6 nodes (t3.small)
- **Services**: audio-source and audio-relay with pod anti-affinity
- **Domain**: audiolab.navicore.tech with ACM TLS certificates
- **Docker Images**: navicore/k8s-audio-audio-source:0.9.0 and navicore/k8s-audio-audio-relay:0.9.0
- **Prometheus**: Basic install in monitoring namespace with web UI

## Prometheus Setup
```bash
# Install (without persistent storage for dev)
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm install prometheus prometheus-community/prometheus \
  --namespace monitoring \
  --create-namespace \
  --set alertmanager.enabled=false \
  --set prometheus-pushgateway.enabled=false \
  --set server.persistentVolume.enabled=false \
  --set server.service.type=ClusterIP

# Access (secure via port-forward)
kubectl port-forward -n monitoring svc/prometheus-server 9090:80
# Then access at http://localhost:9090
# DO NOT expose Prometheus publicly without authentication!
```

### Useful Prometheus Queries for Audio Monitoring
```promql
# Pod network latency
rate(container_network_transmit_packets_total[5m])

# CPU usage by pod
rate(container_cpu_usage_seconds_total{namespace="audio-demo"}[5m])

# Memory usage
container_memory_working_set_bytes{namespace="audio-demo"}

# HTTP request latency (if services export metrics)
histogram_quantile(0.95, rate(http_request_duration_seconds_bucket[5m]))
```

## Known Issues to Fix

### 1. Stuck CloudFormation Stacks
- Deletion gets stuck on "unevictable pods" and VPC dependencies
- Need to add force deletion options or handle stuck resources

### 2. Missing IAM Permission
- Official AWS Load Balancer Controller policy missing `elasticloadbalancing:DescribeListenerAttributes`
- Fixed manually by updating policy, but needs to be in deploy script

### 3. DNS Automation
- Currently requires manual CNAME additions twice:
  - ACM certificate validation records
  - ALB endpoint mappings
- Consider Route53 integration or external-dns operator

### 4. Better Progress Feedback
- ACM validation gives no progress indication
- ALB provisioning status not clear
- Add status checks and progress messages

### 5. Cost Optimization
- ~~Currently using t3.medium (2 vCPU, 4GB RAM) = ~$0.0416/hour~~
- **Updated to t3.small (2 vCPU, 2GB RAM) = ~$0.0208/hour** ✓
- Could try t3.micro (2 vCPU, 1GB RAM) = ~$0.0104/hour if small works well
- Audio services are lightweight, mainly testing network latency
- Saves ~$30/month per node (50% cost reduction)

## Reproducibility Checklist
- [ ] Update deploy script with fixed IAM policy
- [ ] Add DNS automation or clearer instructions
- [ ] Add cluster deletion retry logic
- [ ] Add progress indicators for all wait states
- [ ] Test with smaller instance types
- [ ] Document all required environment variables clearly
- [ ] Add pre-flight checks for AWS permissions

## Quick Commands
```bash
# Deploy
export AWS_REGION=us-east-1
export SUBDOMAIN=audiolab
export BASE_DOMAIN=navicore.tech
export CERT_EMAIL=your-email@example.com
export DOCKER_HUB_USERNAME=navicore
./eks/deploy-eks.sh

# Install app
helm repo add navicore https://www.navicore.tech/charts
helm install audio-lab navicore/audio-lab \
  --set global.domain=audiolab.navicore.tech \
  --set audioSource.ingress.className=alb \
  --set audioRelay.ingress.className=alb \
  --set audioSource.image.tag=0.9.0 \
  --set audioRelay.image.tag=0.9.0

# Destroy
./eks/destroy-eks.sh
```

## Future Improvements
- One-command deploy with all automation
- Automatic DNS updates when cluster recreated
- Cost-optimized node configuration
- Better error handling and recovery