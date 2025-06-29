# EKS Deployment for Audio Lab

This directory contains scripts and configuration for deploying the audio lab application to AWS EKS with TLS support.

## Prerequisites

1. AWS CLI configured with appropriate credentials
2. eksctl installed
3. kubectl installed
4. helm installed
5. A domain name with DNS management access
6. Docker Hub account for storing images

## Environment Variables

Set these before running the deployment:

```bash
export AWS_REGION=us-east-1  # Your preferred region
export DOMAIN_NAME=yourdomain.com  # Your domain
export CERT_EMAIL=admin@yourdomain.com  # Email for certificates
export DOCKER_HUB_USERNAME=yourusername  # Your Docker Hub username
```

## Deployment Steps

1. **Deploy the EKS cluster:**
   ```bash
   ./deploy-eks.sh
   ```

2. **Validate the ACM certificate:**
   - Go to AWS Certificate Manager in the AWS Console
   - Find your certificate request
   - Add the DNS validation records to your domain
   - Wait for validation to complete

3. **Deploy the application:**
   ```bash
   helm install audio-lab ../charts/audio-lab -f values-eks-configured.yaml
   ```

4. **Configure DNS:**
   - Get the ALB DNS names:
     ```bash
     kubectl get ingress -n audio-demo
     ```
   - Create CNAME records:
     - `audio-source.yourdomain.com` → ALB DNS for audio-source
     - `audio-relay.yourdomain.com` → ALB DNS for audio-relay

5. **Access the services:**
   - Audio Source: https://audio-source.yourdomain.com
   - Audio Relay: https://audio-relay.yourdomain.com

## Updating the Application

To update the application with new Docker images:

```bash
helm upgrade audio-lab ../charts/audio-lab -f values-eks-configured.yaml
```

## Destroying the Cluster

To tear down the entire deployment:

```bash
./destroy-eks.sh
```

## Cost Considerations

This deployment creates:
- 2 t3.medium EC2 instances
- Application Load Balancers
- CloudWatch logs
- ACM certificate

Estimated cost: ~$150-200/month (varies by region and usage)

## Security Notes

- The cluster uses IAM roles for service accounts (IRSA)
- TLS is enforced via ALB redirect rules
- Pods run with security contexts (non-root user)
- Network policies can be added for additional security