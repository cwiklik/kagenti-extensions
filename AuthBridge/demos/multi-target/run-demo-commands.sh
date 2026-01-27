##
# Run the commands outlined after initial setup and deployment in `demo.md`
#
#!/usr/bin/env bash

set -eu



## Step 1. Get Token

kubectl exec -it deployment/agent -n authbridge -c agent -- sh -c '
CLIENT_ID=$(cat /shared/client-id.txt)
CLIENT_SECRET=$(cat /shared/client-secret.txt)

TOKEN=$(curl -s http://keycloak-service.keycloak.svc:8080/realms/demo/protocol/openid-connect/token \
  -d "grant_type=client_credentials" \
  -d "client_id=$CLIENT_ID" \
  -d "client_secret=$CLIENT_SECRET" | jq -r ".access_token")

echo "Original token audience:"
echo $TOKEN | cut -d. -f2 | tr "_-" "/+" | { read p; echo "${p}=="; } | base64 -d | jq -r .aud
'

## Step 2. Call Each Target

kubectl exec deployment/agent -n authbridge -c agent -- sh -c '
CLIENT_ID=$(cat /shared/client-id.txt)
CLIENT_SECRET=$(cat /shared/client-secret.txt)
TOKEN=$(curl -s http://keycloak-service.keycloak.svc:8080/realms/demo/protocol/openid-connect/token \
  -d "grant_type=client_credentials" -d "client_id=$CLIENT_ID" -d "client_secret=$CLIENT_SECRET" | jq -r ".access_token")

echo "=== Calling Target Alpha ==="
curl -s -H "Authorization: Bearer $TOKEN" http://target-alpha-service:8081/test
echo ""

echo "=== Calling Target Beta ==="
curl -s -H "Authorization: Bearer $TOKEN" http://target-beta-service:8081/test
echo ""

echo "=== Calling Target Gamma ==="
curl -s -H "Authorization: Bearer $TOKEN" http://target-gamma-service:8081/test
echo ""
'


## Step 3. Check AuthBridge Logs
echo ""
echo "=== AuthBridge Token Exchange Logs ==="
kubectl logs deployment/agent -n authbridge -c envoy-proxy | grep -i "matched\|routes" || echo "(no matching log entries found)"
