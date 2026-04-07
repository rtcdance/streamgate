#!/bin/bash

set -e

echo "🚀 Deploying StreamGate..."

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$PROJECT_ROOT"

ENVIRONMENT=${1:-dev}
NAMESPACE=${2:-streamgate}

echo "📦 Environment: $ENVIRONMENT"
echo "🏷️  Namespace: $NAMESPACE"

case $ENVIRONMENT in
  dev)
    echo "🔨 Deploying to development..."
    kubectl config use-context dev-cluster
    kubectl apply -f deploy/k8s/namespace.yaml
    kubectl apply -f deploy/k8s/configmaps/dev.yaml
    kubectl apply -f deploy/k8s/secrets/dev.yaml
    kubectl apply -f deploy/k8s/monolith/deployment.yaml
    kubectl apply -f deploy/k8s/monolith/service.yaml
    ;;
  test)
    echo "🧪 Deploying to test..."
    kubectl config use-context test-cluster
    kubectl apply -f deploy/k8s/namespace.yaml
    kubectl apply -f deploy/k8s/configmaps/test.yaml
    kubectl apply -f deploy/k8s/secrets/test.yaml
    kubectl apply -f deploy/k8s/monolith/deployment.yaml
    kubectl apply -f deploy/k8s/monolith/service.yaml
    ;;
  prod)
    echo "🚀 Deploying to production..."
    kubectl config use-context prod-cluster
    kubectl apply -f deploy/k8s/namespace.yaml
    kubectl apply -f deploy/k8s/configmaps/prod.yaml
    kubectl apply -f deploy/k8s/secrets/prod.yaml
    kubectl apply -f deploy/k8s/monolith/deployment.yaml
    kubectl apply -f deploy/k8s/monolith/service.yaml
    kubectl apply -f deploy/k8s/monolith/ingress.yaml
    ;;
  *)
    echo "❌ Unknown environment: $ENVIRONMENT"
    echo "Usage: $0 {dev|test|prod} [namespace]"
    exit 1
    ;;
esac

echo "⏳ Waiting for deployment to be ready..."
kubectl rollout status deployment/streamgate-monolith -n $NAMESPACE --timeout=5m

echo "✅ Deployment complete!"
echo ""
echo "To check status:"
echo "  kubectl get pods -n $NAMESPACE"
echo ""
echo "To view logs:"
echo "  kubectl logs -f deployment/streamgate-monolith -n $NAMESPACE"
echo ""
echo "To get service URL:"
echo "  kubectl get svc streamgate-monolith -n $NAMESPACE"
