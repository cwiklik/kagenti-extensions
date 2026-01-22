#!/bin/bash

set -e

# Configuration
CLUSTER=${CLUSTER:-kagenti}
NAMESPACE=${NAMESPACE:-kagenti-webhook-system}
TAG=$(date +%Y%m%d%H%M%S)
IMAGE_NAME=local/kagenti-webhook:${TAG}

echo "=========================================="
echo "Full Webhook Deployment"
echo "=========================================="
echo "Cluster: ${CLUSTER}"
echo "Namespace: ${NAMESPACE}"
echo "Image: ${IMAGE_NAME}"
echo ""

# Step 1: Build and load image
echo "[1/4] Building Docker image..."
docker build -f Dockerfile . --tag ${IMAGE_NAME} --load

echo ""
echo "[2/4] Loading image into kind cluster..."
kind load docker-image --name ${CLUSTER} ${IMAGE_NAME}

# Step 2: Update deployment
echo ""
echo "[3/4] Updating deployment..."
kubectl -n ${NAMESPACE} set image deployment/kagenti-webhook-controller-manager manager=${IMAGE_NAME}

echo ""
echo "Waiting for rollout to complete..."
kubectl rollout status -n ${NAMESPACE} deployment/kagenti-webhook-controller-manager --timeout=120s

# Step 3: Deploy authbridge webhook if it doesn't exist
echo ""
echo "[4/4] Ensuring authbridge webhook configuration exists..."
if kubectl get mutatingwebhookconfigurations kagenti-webhook-authbridge-mutating-webhook-configuration &>/dev/null; then
    echo "Authbridge webhook already exists, skipping..."
else
    echo "Creating authbridge webhook configuration..."
    kubectl apply -f - <<EOF
apiVersion: admissionregistration.k8s.io/v1
kind: MutatingWebhookConfiguration
metadata:
  name: kagenti-webhook-authbridge-mutating-webhook-configuration
  annotations:
    cert-manager.io/inject-ca-from: ${NAMESPACE}/kagenti-webhook-serving-cert
webhooks:
- name: inject.kagenti.io
  admissionReviewVersions:
  - v1
  clientConfig:
    service:
      name: kagenti-webhook-webhook-service
      namespace: ${NAMESPACE}
      path: /mutate-workloads-authbridge
      port: 443
  failurePolicy: Fail
  timeoutSeconds: 10
  sideEffects: None
  namespaceSelector:
    matchExpressions:
      # Exclude kube-system and other critical namespaces
      - key: kubernetes.io/metadata.name
        operator: NotIn
        values:
          - kube-system
          - kube-public
          - kube-node-lease
          - ${NAMESPACE}
    matchLabels:
      # Only trigger webhook for namespaces that have opted-in
      # This aligns with NeedsMutation() which requires kagenti-enabled: true
      kagenti-enabled: "true"
  rules:
  - operations:
    - CREATE
    - UPDATE
    apiGroups:
    - apps
    apiVersions:
    - v1
    resources:
    - deployments
    - statefulsets
    - daemonsets
  - operations:
    - CREATE
    - UPDATE
    apiGroups:
    - batch
    apiVersions:
    - v1
    resources:
    - jobs
    - cronjobs
EOF
    echo "Waiting for cert-manager to inject CA bundle..."
    sleep 5
fi

echo ""
echo "=========================================="
echo "Deployment Complete!"
echo "=========================================="
echo ""
echo "Current pods:"
kubectl get -n ${NAMESPACE} pod -l control-plane=controller-manager
echo ""
echo "Webhook configurations:"
kubectl get mutatingwebhookconfigurations | grep kagenti-webhook
echo ""
echo "To view logs:"
echo "  kubectl logs -n ${NAMESPACE} -l control-plane=controller-manager -f"
