#!/bin/bash
set -e

# Script to destroy EKS cluster and associated resources

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Configuration
CLUSTER_NAME="audio-lab-cluster-2"
REGION="${AWS_REGION:-us-east-1}"

echo -e "${YELLOW}WARNING: This will destroy the EKS cluster and all resources!${NC}"
read -p "Are you sure you want to continue? (yes/no): " confirm

if [ "$confirm" != "yes" ]; then
    echo -e "${GREEN}Destruction cancelled.${NC}"
    exit 0
fi

echo -e "${RED}Starting EKS cluster destruction...${NC}"

# Delete Helm releases
echo -e "${YELLOW}Deleting Helm releases...${NC}"
helm list -A | grep -E "audio-lab|aws-load-balancer-controller" | awk '{print $1, $2}' | while read name namespace; do
    echo "Deleting $name from namespace $namespace"
    helm uninstall $name -n $namespace || true
done

# Delete the cluster
echo -e "${YELLOW}Deleting EKS cluster...${NC}"
eksctl delete cluster --name $CLUSTER_NAME --region $REGION

# Clean up IAM resources
echo -e "${YELLOW}Cleaning up IAM resources...${NC}"
aws iam delete-policy --policy-arn arn:aws:iam::$(aws sts get-caller-identity --query Account --output text):policy/AWSLoadBalancerControllerIAMPolicy 2>/dev/null || true

echo -e "${GREEN}EKS cluster destruction complete!${NC}"
echo -e "${YELLOW}Note: ACM certificates must be deleted manually from the AWS Console.${NC}"
