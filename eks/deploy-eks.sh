#!/bin/bash
set -e

# Script to deploy EKS cluster with ALB controller and cert-manager for TLS

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Configuration
CLUSTER_NAME="audio-lab-cluster"
REGION="${AWS_REGION:-us-east-1}"
SUBDOMAIN="${SUBDOMAIN:-audiolab}"
BASE_DOMAIN="${BASE_DOMAIN:-example.com}"
DOMAIN_NAME="${SUBDOMAIN}.${BASE_DOMAIN}"
CERT_EMAIL="${CERT_EMAIL:-admin@example.com}"

echo -e "${GREEN}Starting EKS cluster deployment...${NC}"

# Check required environment variables
echo -e "${YELLOW}Checking environment variables...${NC}"
if [ -z "$BASE_DOMAIN" ]; then
    echo -e "${RED}ERROR: BASE_DOMAIN environment variable is not set${NC}"
    echo -e "${RED}Please set: export BASE_DOMAIN=navicore.tech${NC}"
    exit 1
fi

if [ -z "$CERT_EMAIL" ]; then
    echo -e "${RED}ERROR: CERT_EMAIL environment variable is not set${NC}"
    echo -e "${RED}Please set: export CERT_EMAIL=your-email@example.com${NC}"
    exit 1
fi

if [ -z "$DOCKER_HUB_USERNAME" ]; then
    echo -e "${RED}ERROR: DOCKER_HUB_USERNAME environment variable is not set${NC}"
    echo -e "${RED}Please set: export DOCKER_HUB_USERNAME=yourusername${NC}"
    exit 1
fi

echo -e "${GREEN}Environment variables OK:${NC}"
echo -e "  AWS_REGION: ${REGION}"
echo -e "  SUBDOMAIN: ${SUBDOMAIN}"
echo -e "  BASE_DOMAIN: ${BASE_DOMAIN}"
echo -e "  DOMAIN_NAME: ${DOMAIN_NAME}"
echo -e "  CERT_EMAIL: ${CERT_EMAIL}"
echo -e "  DOCKER_HUB_USERNAME: ${DOCKER_HUB_USERNAME}"

# Check prerequisites
echo -e "${YELLOW}Checking prerequisites...${NC}"
command -v aws >/dev/null 2>&1 || { echo -e "${RED}AWS CLI is required but not installed. Aborting.${NC}" >&2; exit 1; }
command -v eksctl >/dev/null 2>&1 || { echo -e "${RED}eksctl is required but not installed. Aborting.${NC}" >&2; exit 1; }
command -v kubectl >/dev/null 2>&1 || { echo -e "${RED}kubectl is required but not installed. Aborting.${NC}" >&2; exit 1; }
command -v helm >/dev/null 2>&1 || { echo -e "${RED}Helm is required but not installed. Aborting.${NC}" >&2; exit 1; }

# Create EKS cluster
echo -e "${GREEN}Creating EKS cluster...${NC}"
eksctl create cluster -f cluster-config.yaml

# Update kubeconfig
echo -e "${GREEN}Updating kubeconfig...${NC}"
aws eks update-kubeconfig --name $CLUSTER_NAME --region $REGION

# Install AWS Load Balancer Controller
echo -e "${GREEN}Installing AWS Load Balancer Controller...${NC}"

# Create IAM policy for ALB controller
curl -o iam_policy.json https://raw.githubusercontent.com/kubernetes-sigs/aws-load-balancer-controller/v2.6.2/docs/install/iam_policy.json
aws iam create-policy \
    --policy-name AWSLoadBalancerControllerIAMPolicy \
    --policy-document file://iam_policy.json 2>/dev/null || echo "Policy already exists"

# Create service account
eksctl create iamserviceaccount \
  --cluster=$CLUSTER_NAME \
  --namespace=kube-system \
  --name=aws-load-balancer-controller \
  --role-name AmazonEKSLoadBalancerControllerRole \
  --attach-policy-arn=arn:aws:iam::$(aws sts get-caller-identity --query Account --output text):policy/AWSLoadBalancerControllerIAMPolicy \
  --approve

# Install cert-manager (required by ALB controller)
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.13.3/cert-manager.yaml

# Wait for cert-manager to be ready
echo -e "${YELLOW}Waiting for cert-manager to be ready...${NC}"
kubectl wait --for=condition=available --timeout=300s deployment/cert-manager -n cert-manager
kubectl wait --for=condition=available --timeout=300s deployment/cert-manager-webhook -n cert-manager
kubectl wait --for=condition=available --timeout=300s deployment/cert-manager-cainjector -n cert-manager

# Add eks-charts repo
helm repo add eks https://aws.github.io/eks-charts
helm repo update

# Install AWS Load Balancer Controller
helm install aws-load-balancer-controller eks/aws-load-balancer-controller \
  -n kube-system \
  --set clusterName=$CLUSTER_NAME \
  --set serviceAccount.create=false \
  --set serviceAccount.name=aws-load-balancer-controller

# Create ACM certificate (you need to validate this in AWS Console)
echo -e "${GREEN}Creating ACM certificate request...${NC}"
CERTIFICATE_ARN=$(aws acm request-certificate \
  --domain-name "*.${DOMAIN_NAME}" \
  --subject-alternative-names "audio-source.${DOMAIN_NAME}" "audio-relay.${DOMAIN_NAME}" \
  --validation-method DNS \
  --region $REGION \
  --query CertificateArn \
  --output text)

echo -e "${YELLOW}Certificate ARN: ${CERTIFICATE_ARN}${NC}"
echo -e "${RED}IMPORTANT: You must validate this certificate in the AWS Console before proceeding!${NC}"
echo -e "${RED}Go to ACM in AWS Console and add the DNS validation records to your domain.${NC}"

# Wait for user confirmation
read -p "Press enter once you have validated the certificate..."

# Create values file with certificate ARN
cat > values-eks-configured.yaml <<EOF
# Auto-generated EKS values file
audioSource:
  image:
    repository: ${DOCKER_HUB_USERNAME}/k8s-audio-audio-source
    tag: latest
  service:
    type: ClusterIP
  ingress:
    enabled: true
    className: "alb"
    annotations:
      alb.ingress.kubernetes.io/scheme: internet-facing
      alb.ingress.kubernetes.io/target-type: ip
      alb.ingress.kubernetes.io/certificate-arn: "${CERTIFICATE_ARN}"
      alb.ingress.kubernetes.io/listen-ports: '[{"HTTP": 80}, {"HTTPS": 443}]'
      alb.ingress.kubernetes.io/ssl-redirect: '443'
    hosts:
      - host: audio-source.${DOMAIN_NAME}
        paths:
          - path: /
            pathType: Prefix

audioRelay:
  image:
    repository: ${DOCKER_HUB_USERNAME}/k8s-audio-audio-relay
    tag: latest
  service:
    type: ClusterIP
  ingress:
    enabled: true
    className: "alb"
    annotations:
      alb.ingress.kubernetes.io/scheme: internet-facing
      alb.ingress.kubernetes.io/target-type: ip
      alb.ingress.kubernetes.io/certificate-arn: "${CERTIFICATE_ARN}"
      alb.ingress.kubernetes.io/listen-ports: '[{"HTTP": 80}, {"HTTPS": 443}]'
      alb.ingress.kubernetes.io/ssl-redirect: '443'
    hosts:
      - host: audio-relay.${DOMAIN_NAME}
        paths:
          - path: /
            pathType: Prefix
EOF

echo -e "${GREEN}EKS cluster deployment complete!${NC}"
echo -e "${YELLOW}Next steps:${NC}"
echo "1. Deploy the audio-lab helm chart:"
echo "   helm install audio-lab ../charts/audio-lab -f values-eks-configured.yaml"
echo "2. Get the ALB DNS names and create CNAME records in your DNS:"
echo "   kubectl get ingress -n audio-demo"
echo "3. Access your services at:"
echo "   https://audio-source.${DOMAIN_NAME}"
echo "   https://audio-relay.${DOMAIN_NAME}"