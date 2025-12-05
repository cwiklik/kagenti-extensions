#!/bin/bash
# cleanup-kagenti-webhook.sh

WEBHOOK_NAMESPACE="kagenti-webhook-system"

echo "=== Cleaning up kagenti-webhook-system webhooks only ==="

# Find webhook configurations that reference kagenti-webhook-system
echo "Finding webhook configurations..."
MUTATING_WEBHOOKS=$(kubectl get mutatingwebhookconfigurations -o json | \
			jq -r '.items[] | select(.webhooks[].clientConfig.service.namespace=="'$WEBHOOK_NAMESPACE'") | .metadata.name')

VALIDATING_WEBHOOKS=$(kubectl get validatingwebhookconfigurations -o json | \
  jq -r '.items[] | select(.webhooks[].clientConfig.service.namespace=="'$WEBHOOK_NAMESPACE'") | .metadata.name')

# Delete mutating webhooks
for webhook in $MUTATING_WEBHOOKS; do
  echo "Deleting mutating webhook: $webhook"
  kubectl delete mutatingwebhookconfiguration $webhook
done


# Delete validating webhooks
for webhook in $VALIDATING_WEBHOOKS; do
    echo "Deleting validating webhook: $webhook"
    kubectl delete validatingwebhookconfiguration $webhook
done

# Delete deployments in kagenti-webhook-system namespace
echo "Deleting deployments in $WEBHOOK_NAMESPACE..."
kubectl delete deployment -n $WEBHOOK_NAMESPACE --all

# Delete deployments in kagenti-webhook-system namespace
echo "Deleting statefulsets in $WEBHOOK_NAMESPACE..."
kubectl delete statefulsets -n $WEBHOOK_NAMESPACE --all

# Delete services in kagenti-webhook-system namespace
echo "Deleting services in $WEBHOOK_NAMESPACE..."
kubectl delete service -n $WEBHOOK_NAMESPACE --all

# Delete pods in kagenti-webhook-system namespace
echo "Deleting pods in $WEBHOOK_NAMESPACE..."
kubectl delete pods -n $WEBHOOK_NAMESPACE --all

# Delete webhook certificates
echo "Deleting webhook certificates in $WEBHOOK_NAMESPACE..."
kubectl delete secret -n $WEBHOOK_NAMESPACE webhook-server-cert 2>/dev/null || true

echo "=== Cleanup complete for $WEBHOOK_NAMESPACE ==="~
