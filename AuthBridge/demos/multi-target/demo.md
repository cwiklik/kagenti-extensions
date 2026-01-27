# Multi-Target Demo

This demo shows AuthBridge performing route-based token exchange to multiple target services.

## Overview

```
Agent Pod                                    Target Pods
+------------------+                         +------------------+
|                  |                         |  target-alpha    |
|   agent          |    token exchange       |  aud: target-alpha
|                  |------------------------>+------------------+
|                  |         |
|   AuthProxy      |         |               +------------------+
|   sidecar        |---------|-------------->|  target-beta     |
|                  |         |               |  aud: target-beta |
+------------------+         |               +------------------+
                             |
                             |               +------------------+
                             +-------------->|  target-gamma    |
                                             |  aud: target-gamma
                                             +------------------+
```

The AuthProxy automatically exchanges tokens based on the destination host:

| Host | Target Audience | Scope |
|------|-----------------|-------|
| `target-alpha-service.authbridge.svc.cluster.local` | `target-alpha` | `target-alpha-aud` |
| `target-beta-service.authbridge.svc.cluster.local` | `target-beta` | `target-beta-aud` |
| `target-gamma-service.authbridge.svc.cluster.local` | `target-gamma` | `target-gamma-aud` |

## Prerequisites

- Kubernetes cluster with SPIRE installed
- Keycloak running in the cluster
- AuthBridge images built and loaded

## Setup

### 1. Sync Routes with Keycloak

Port-forward Keycloak:

```bash
kubectl port-forward service/keycloak-service -n keycloak 8080:8080
```

Use `keycloak_sync.py` to reconcile the routes configuration with Keycloak:

```bash
cd AuthBridge

# Create virtual environment (if not already done)
python -m venv venv
source venv/bin/activate
pip install -r requirements.txt

# Dry run first to see what would be created
python keycloak_sync.py --config demos/multi-target/routes.yaml --dry-run

# Apply changes (interactive prompts)
# Use --agent-client to assign scopes to the agent that will perform token exchange
python keycloak_sync.py --config demos/multi-target/routes.yaml \
  --agent-client "spiffe://localtest.me/ns/authbridge/sa/agent"

# Or auto-approve all changes
python keycloak_sync.py --config demos/multi-target/routes.yaml \
  --agent-client "spiffe://localtest.me/ns/authbridge/sa/agent" --yes
```

This reconciles routes.yaml with Keycloak, creating:
- `target-alpha`, `target-beta`, `target-gamma` clients (targets)
- Audience scopes (`target-alpha-aud`, etc.) with audience mappers
- Hostname attributes on each target client
- Assigns scopes to the agent client (so it can request tokens for each audience)

The agent client is created automatically by client-registration during deployment.

### 2. Build Images

Build the AuthProxy images (includes the ext-proc with route-based exchange):

```bash
cd AuthProxy

# Build all images
make build-images

# Load into Kind (default cluster name is "kagenti")
make load-images

# Or specify a different cluster name
make load-images KIND_CLUSTER_NAME=<your-cluster-name>
```

This builds:
- `envoy-with-processor` - Envoy + ext-proc (go-processor with TargetResolver)
- `demo-app` - Target service that validates JWT audience
- `auth-proxy` - Auth proxy container
- `proxy-init` - iptables init container

### 3. Deploy the Demo

Deploy everything (agent, targets, routes):

```bash
# With SPIFFE (requires SPIRE)
kubectl apply -f demos/multi-target/k8s/authbridge-deployment.yaml
kubectl apply -f demos/multi-target/k8s/targets.yaml

# OR without SPIFFE
kubectl apply -f demos/multi-target/k8s/authbridge-deployment-no-spiffe.yaml
kubectl apply -f demos/multi-target/k8s/targets.yaml
```

This deploys:
- Agent pod with AuthProxy sidecar
- Routes ConfigMap with multi-target configuration
- Three target service pods (alpha, beta, gamma)

### 4. Wait for Pods

```bash
kubectl wait --for=condition=available --timeout=180s deployment/agent -n authbridge
kubectl wait --for=condition=available --timeout=120s deployment/target-alpha -n authbridge
kubectl wait --for=condition=available --timeout=120s deployment/target-beta -n authbridge
kubectl wait --for=condition=available --timeout=120s deployment/target-gamma -n authbridge
```

## Test the Flow

### Get a Token

```bash
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
```

### Call Each Target

The AuthProxy exchanges the token for the appropriate audience based on the destination host:

```bash
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
```

Expected output:
```
=== Calling Target Alpha ===
authorized
=== Calling Target Beta ===
authorized
=== Calling Target Gamma ===
authorized
```

## Verify Token Exchange

Check the envoy-proxy logs to see the token exchange in action:

```bash
kubectl logs deployment/agent -n authbridge -c envoy-proxy | grep -i "matched\|routes"
```

Expected output:
```
[Routes] Host "target-alpha-service" matched pattern "target-alpha-service"
[Routes] Host "target-beta-service" matched pattern "target-beta-service"
[Routes] Host "target-gamma-service" matched pattern "target-gamma-service"
```

## How It Works

1. Agent obtains a token from Keycloak (audience: agent's SPIFFE ID)
2. Agent makes HTTP request to a target service
3. Envoy intercepts the request and sends headers to ext-proc
4. ext-proc looks up the route configuration in `routes.yaml`
5. ext-proc exchanges the token for one with the target's audience
6. Envoy forwards the request with the exchanged token
7. Target validates the token and returns "authorized"

## Routes Configuration

The `routes.yaml` file maps hosts to token exchange parameters:

```yaml
# Target Alpha
- host: "target-alpha-service.authbridge.svc.cluster.local"
  target_audience: "target-alpha"
  token_scopes: "openid target-alpha-aud"

# Target Beta
- host: "target-beta-service.authbridge.svc.cluster.local"
  target_audience: "target-beta"
  token_scopes: "openid target-beta-aud"

# Target Gamma
- host: "target-gamma-service.authbridge.svc.cluster.local"
  target_audience: "target-gamma"
  token_scopes: "openid target-gamma-aud"
```

## Cleanup

```bash
kubectl delete -f demos/multi-target/k8s/authbridge-deployment.yaml
kubectl delete -f demos/multi-target/k8s/
```

Or delete the entire namespace:

```bash
kubectl delete namespace authbridge
```
