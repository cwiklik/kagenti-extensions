#!/bin/bash

# Check if all required environment variables are set
missing_vars=()
for var in TOKEN_URL CLIENT_ID CLIENT_SECRET TARGET_AUDIENCE TARGET_SCOPES; do
  if [ -z "${!var}" ]; then
    missing_vars+=("$var")
  fi
done

if [ ${#missing_vars[@]} -gt 0 ]; then
  echo "Error: The following required environment variables are not set:"
  printf '  - %s\n' "${missing_vars[@]}"
  exit 1
fi

# Create the secret
kubectl create secret generic auth-proxy-config \
  --from-literal=TOKEN_URL="$TOKEN_URL" \
  --from-literal=CLIENT_ID="$CLIENT_ID" \
  --from-literal=CLIENT_SECRET="$CLIENT_SECRET" \
  --from-literal=TARGET_AUDIENCE="$TARGET_AUDIENCE" \
  --from-literal=TARGET_SCOPES="$TARGET_SCOPES" \
  --dry-run=client -o yaml | kubectl apply -f -

echo "Secret 'auth-proxy-config' created/updated successfully"

#Usage:
#export TOKEN_URL="http://keycloak.keycloak.svc.cluster.local:8080/realms/master/protocol/openid-connect/token"
#export CLIENT_ID="spiffe://localtest.me/sa/slack-researcher"
#export CLIENT_SECRET="xxxxx"
#export TARGET_AUDIENCE="spiffe://localtest.me/sa/slack-tool"
#export TARGET_SCOPES="slack-full-access slack-partial-access"
#./create-secret.sh
