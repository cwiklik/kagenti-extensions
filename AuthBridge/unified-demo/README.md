# Unified AuthBridge Demo

This demo combines the **Client Registration** and **AuthProxy** components to demonstrate a complete end-to-end authentication flow with SPIFFE/SPIRE integration.

## Architecture

```
┌────────────────────────────────────────────────────────────────────────┐
│                           CALLER POD                                   │
│                                                                        │
│  ┌─────────────────────────────────────────────────────────────────┐   │
│  │  Init Container: proxy-init (iptables setup)                    │   │
│  └─────────────────────────────────────────────────────────────────┘   │
│                                                                        │
│  ┌─────────────────────────────────────────────────────────────────┐   │
│  │                      Containers                                 │   │
│  │  ┌──────────────┐  ┌─────────────────┐  ┌────────────────────┐  │   │
│  │  │   BusyBox    │  │  SPIFFE Helper  │  │    AuthProxy +     │  │   │
│  │  │   (Caller)   │  │  (provides      │  │    Envoy + Go Proc │  │   │
│  │  │              │  │   SPIFFE creds) │  │  (token exchange)  │  │   │
│  │  └──────┬───────┘  └─────────────────┘  └──────────┬─────────┘  │   │
│  │                                                                 │   │
│  │  ┌───────────────────────────────────────────────────────────┐  │   │
│  │  │ client-registration (registers with Keycloak using SPIFFE)│  │   │
│  │  └───────────────────────────────────────────────────────────┘  │   │
│  └─────────┼───────────────────────────────────────────┼───────────┘   │
│            │ HTTP request with token                   │               │
│            └───────────────────────────────────────────┘               │
│                              │                                         │
└──────────────────────────────┼─────────────────────────────────────────┘
                               │ Token exchanged for demoapp audience
                               ▼
                    ┌─────────────────────┐
                    │    DEMO APP POD     │
                    │   (Target Server)   │
                    │                     │
                    │  Validates token    │
                    │  with audience      │
                    │  "demoapp"          │
                    └─────────────────────┘
```

### Token Flow

1. **Client Registration** (init container) uses the **SPIFFE ID** to register the caller workload with Keycloak
2. **BusyBox** (caller) obtains a token from Keycloak using the auto-registered client credentials
3. **AuthProxy + Envoy** (sidecar) intercepts the outgoing request and exchanges the token for one with audience `demoapp`
4. **Demo App** (target server) validates the exchanged token

### Components in Caller Pod

| Container | Type | Purpose |
|-----------|------|---------|
| `proxy-init` | init | Sets up iptables to intercept outgoing traffic |
| `client-registration` | container | Registers workload with Keycloak using SPIFFE ID (waits for SPIFFE Helper) |
| `caller` (curl) | container | The application making requests |
| `spiffe-helper` | container | Provides SPIFFE credentials (SVID) |
| `auth-proxy` | container | Validates tokens |
| `envoy-proxy` | container | Intercepts traffic and performs token exchange via go-processor |

## Prerequisites

- Kubernetes cluster (Kind recommended for local development)
- SPIRE installed and running (server + agent) - for SPIFFE version
- Keycloak deployed
- Docker/Podman for building images

### Quick Setup with Kagenti

The easiest way to get all prerequisites is to use the [Kagenti Ansible installer](https://github.com/kagenti/kagenti/blob/main/docs/install.md#ansible-based-installer-recommended) and then follow the instructions below.

## Step-by-Step Guide

### 1. Build and Load Images

Navigate to the AuthProxy directory and build the required images:

```bash
cd AuthBridge/AuthProxy

# Build all images
make build-images

# Load images into Kind cluster
make load-images
```

### 2. Install SPIRE (if not using Kagenti install)

```bash
# Install SPIRE CRDs
helm upgrade --install spire-crds spire-crds \
  -n spire-mgmt \
  --repo https://spiffe.github.io/helm-charts-hardened/ \
  --create-namespace --wait

# Install SPIRE
helm upgrade --install spire spire \
  -n spire-mgmt \
  --repo https://spiffe.github.io/helm-charts-hardened/ \
  -f https://raw.githubusercontent.com/kagenti/kagenti/main/kagenti/installer/app/resources/spire-helm-values.yaml
```

### 3. Install Keycloak (if not using Kagenti install)

```bash
# Create namespace
kubectl apply -f "https://raw.githubusercontent.com/kagenti/kagenti/refs/heads/main/kagenti/installer/app/resources/keycloak-namespace.yaml"

# Deploy Keycloak
kubectl apply -f "https://raw.githubusercontent.com/kagenti/kagenti/refs/heads/main/kagenti/installer/app/resources/keycloak.yaml" -n keycloak

# Wait for Keycloak to be ready
kubectl rollout status statefulset/keycloak -n keycloak --timeout=120s
```

### 4. Configure Keycloak

Port-forward Keycloak to access it locally:

```bash
kubectl port-forward service/keycloak -n keycloak 8080:8080
```

In a new terminal, run the setup script:

```bash
cd AuthBridge/unified-demo

# Create virtual environment
python -m venv venv
source venv/bin/activate

# Install dependencies
pip install --upgrade pip
pip install -r requirements.txt

# Run setup script
python setup_keycloak.py
```

The script will:
- Create the `demo` realm
- Create the `authproxy` client (for token exchange)
- Create audience scopes (`authproxy-aud`, `demoapp-aud`)

**Important:** Note the `authproxy` client secret from the output.

### 5. Deploy the Unified Demo

First, deploy the `auth-proxy-config` secret:

```bash
kubectl create -f k8s/auth-proxy-config.yaml
```

Then update this secret with the `authproxy` client secret obtained above in the Configure Keycloak step:

```bash
# Replace AUTHPROXY_SECRET with the actual secret from setup_keycloak.py output
kubectl patch secret auth-proxy-config -p '{"stringData":{"CLIENT_SECRET":"AUTHPROXY_SECRET"}}'
```

You might need to configure ghcr secret for accessing images. Simply copy secret from Kagenti `team1` namespace to `default`:

```bash
kubectl get secret ghcr-secret  -n team1 -o yaml | sed 's/namespace: source-ns/namespace: target-ns/' > ghcr-secret.yaml
# replace the namespace to default
kubectl create -f ghcr-secret.yaml
```

Deploy the unified stack:

```bash
# With SPIFFE (requires SPIRE)
kubectl apply -f k8s/unified-deployment.yaml

# OR without SPIFFE
kubectl apply -f k8s/unified-deployment-no-spiffe.yaml
```

Wait for deployments to be ready:

```bash
kubectl wait --for=condition=available --timeout=120s deployment/caller
kubectl wait --for=condition=available --timeout=120s deployment/demo-app
```

### 6. Test the Flow

Exec into the caller pod and make a request:

```bash
# Exec into the caller container
kubectl exec -it deployment/caller -c caller -- sh

# Inside the container:

# Read the auto-generated client secret and client id
CLIENT_SECRET=$(cat /shared/client-secret.txt)
CLIENT_ID=$(cat /shared/client-id.txt)
echo "Client secret: $CLIENT_SECRET"
echo "Client id: $CLIENT_ID"

TOKEN=$(curl -sX POST http://keycloak-service.keycloak.svc:8080/realms/demo/protocol/openid-connect/token \
  -d 'grant_type=client_credentials' \
  -d "client_id=$CLIENT_ID" \
  -d "client_secret=$CLIENT_SECRET" | jq -r '.access_token')

curl -H "Authorization: Bearer $TOKEN" http://demo-app-service:8081/test


# Get the client ID (SPIFFE ID or "caller" for no-spiffe version)
# For SPIFFE version, check Keycloak for the registered client ID


# Get a token from Keycloak
TOKEN=$(curl -sX POST \
  http://keycloak-service.keycloak.svc:8080/realms/demo/protocol/openid-connect/token \
  -d 'grant_type=client_credentials' \
  -d 'client_id=caller' \
  -d "client_secret=$CLIENT_SECRET" | jq -r '.access_token')

TOKEN=$(curl -sX POST \
  http://keycloak.keycloak.svc:8080/realms/demo/protocol/openid-connect/token \
  -d 'grant_type=client_credentials' \
  -d 'client_id=caller' \
  -d "client_secret=$CLIENT_SECRET" | jq -r '.access_token')  

echo "Token obtained!"

# Call the demo-app through AuthProxy
# The outgoing request will be intercepted by Envoy, which exchanges the token
curl -H "Authorization: Bearer $TOKEN" http://demo-app-service:8081/test

# Expected output: "authorized"
```

## Verification

### Verify SPIFFE Client Registration

Check Keycloak for the auto-registered client:

1. Go to http://keycloak.localtest.me:8080
2. Login with `admin` / `admin`
3. Switch to realm `demo`
4. Navigate to **Clients**
5. Look for a client with a SPIFFE ID as the Client ID (e.g., `spiffe://example.org/ns/default/sa/default`)

### View Logs

```bash
# Caller pod logs
kubectl logs deployment/caller -c caller
kubectl logs deployment/caller -c auth-proxy
kubectl logs deployment/caller -c envoy-proxy
kubectl logs deployment/caller -c spiffe-helper

# Client registration logs (init container)
kubectl logs deployment/caller -c client-registration

# Demo app logs
kubectl logs deployment/demo-app
```

### Test Invalid Scenarios

From inside the caller container:

```bash
# Invalid token (should fail at demo-app)
curl -H "Authorization: Bearer invalid-token" http://demo-app-service:8081/test
# Expected: 401 Unauthorized

# No authorization
curl http://demo-app-service:8081/test
# Expected: 401 Unauthorized
```

## How It Works

### 1. Client Registration (Init Container)

When the caller pod starts:
1. Waits for SPIFFE credentials from the SPIFFE Helper sidecar
2. Reads the SVID JWT to extract the SPIFFE ID
3. Registers with Keycloak using the SPIFFE ID as the client ID
4. Writes the generated client secret to `/shared/client-secret.txt`

### 2. AuthProxy Sidecar

The AuthProxy runs alongside the caller application:
- **proxy-init** sets up iptables to redirect outgoing traffic to Envoy
- **Envoy** intercepts HTTP requests on port 15123
- **go-processor** (external processor) performs token exchange:
  - Extracts the Bearer token from the Authorization header
  - Exchanges it with Keycloak for a new token with audience `demoapp`
  - Replaces the Authorization header with the new token
- The request continues to the original destination (demo-app)

### 3. Demo App (Target Server)

The demo app:
- Validates the JWT token against Keycloak's JWKS
- Checks that the issuer and audience claims match
- Returns "authorized" for valid tokens

## Cleanup

```bash
# Remove deployments
kubectl delete -f k8s/unified-deployment.yaml
# OR
kubectl delete -f k8s/unified-deployment-no-spiffe.yaml
```

## Troubleshooting

### SPIFFE Helper Not Starting

Check if SPIRE agent is running:
```bash
kubectl get pods -n spire-mgmt
```

Verify the agent socket path:
```bash
kubectl exec -it deployment/caller -c spiffe-helper -- ls -la /spiffe-workload-api/
```

### Client Registration Fails

Check the init container logs:
```bash
kubectl logs deployment/caller -c client-registration
```

Verify Keycloak connectivity from within the cluster:
```bash
kubectl run test-curl --rm -it --image=curlimages/curl -- \
  curl -s http://keycloak.keycloak.svc:8080/realms/demo/.well-known/openid-configuration
```

### Token Exchange Fails

Check the envoy-proxy logs:
```bash
kubectl logs deployment/caller -c envoy-proxy
```

Verify the auth-proxy-config secret has the correct values:
```bash
kubectl get secret auth-proxy-config -o yaml
```

### Demo App Rejects Token

Check that the token has the correct audience:
```bash
# Decode the token (inside caller container)
echo $TOKEN | cut -d'.' -f2 | base64 -d 2>/dev/null | jq .
# Look for "aud": ["demoapp", ...]
```

## References

- [AuthBridge Client Registration](../../../client-registration/README.md)
- [AuthProxy Quickstart](../README.md)
- [Kagenti Installation](https://github.com/kagenti/kagenti/blob/main/docs/install.md)
- [SPIRE Documentation](https://spiffe.io/docs/latest/)
