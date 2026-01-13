# AuthProxy

AuthProxy is a **JWT validation and token exchange proxy** for Kubernetes workloads. It enables secure service-to-service communication by intercepting outgoing requests, validating tokens, and transparently exchanging them for tokens with the correct audience for downstream services.

## What AuthProxy Does

AuthProxy solves a common challenge in microservices architectures: **how can a service call another service when each service expects tokens with different audiences?**

### The Problem

When a caller obtains a token, it's typically scoped to a specific audience (often the caller itself). If the caller tries to use that token to call a different service, the request will be rejected because the target service expects a different audience.

```
┌─────────────┐                      ┌──────────────┐
│   Caller    │ ── Token A ────────► │   Target     │  ❌ REJECTED
│ (aud: svc-a)│                      │ (expects     │     Wrong audience!
└─────────────┘                      │  aud: target)│
                                     └──────────────┘
```

### The Solution

AuthProxy intercepts outgoing requests, validates the caller's token, and exchanges it for a new token with the correct audience:

```
┌─────────────┐               ┌──────────────────────────┐              ┌─────────────┐
│   Caller    │ ── Token A ──►│       AuthProxy          │─ Token B ──► │   Target    │  ✅ AUTHORIZED
│             │               │  1. Validate token       │              │             │
│ Token:      │               │  2. Exchange for new aud │              │ (expects    │
│ (aud: svc-a)│               │  3. Forward request      │              │ aud: target)│
└─────────────┘               └──────────────────────────┘              └─────────────┘
                                           │
                                           ▼
                                  ┌─────────────────┐
                                  │    Keycloak     │
                                  │ (Token Exchange)│
                                  └─────────────────┘
```

## Components

AuthProxy consists of two main components that work together:

### 1. AuthProxy (`main.go`)

A Go HTTP proxy that:
- Receives incoming requests on port **8080**
- Validates JWT tokens using JWKS (JSON Web Key Set)
- Checks token claims: `issuer`, `audience` (optional), and `scope` (optional)
- Forwards validated requests to the target service
- Returns `401 Unauthorized` for invalid tokens

### 2. Ext Proc - External Processor (`go-processor/main.go`)

An Envoy external processor (gRPC) that:
- Runs on port **9090**
- Intercepts HTTP requests via Envoy
- Performs **OAuth 2.0 Token Exchange** ([RFC 8693](https://datatracker.ietf.org/doc/html/rfc8693))
- Replaces the `Authorization` header with the exchanged token
- Works transparently—the caller doesn't know token exchange happened

## Architecture

### Sidecar Deployment (Recommended)

When deployed as a sidecar, AuthProxy intercepts all outgoing traffic from the application:

```
┌─────────────────────────────────────────────────────────────────┐
│                         POD                                     │
│  ┌──────────────┐    ┌─────────────────────────────────────┐    │
│  │              │    │         AuthProxy Sidecar           │    │
│  │  Application │    │  ┌───────────┐    ┌──────────────┐  │    │
│  │              │───►│  │  Envoy    │───►│  Ext Proc    │  │    │
│  │              │    │  │  :15123   │    │    :9090     │  │    │
│  └──────────────┘    │  └───────────┘    └──────────────┘  │    │
│         │            │        │                 │          │    │
│         │            │        ▼                 ▼          │    │
│  ┌──────┴───────┐    │  ┌───────────┐    ┌──────────────┐  │    │
│  │  proxy-init  │    │  │AuthProxy  │    │   Keycloak   │  │    │
│  │ (iptables)   │    │  │   :8080   │    │ (exchange)   │  │    │
│  └──────────────┘    │  └───────────┘    └──────────────┘  │    │
│                      └─────────────────────────────────────┘    │
└─────────────────────────────────────────────────────────────────┘
```

**Components:**
- **proxy-init**: Init container that sets up iptables to redirect outbound traffic to Envoy
- **Envoy**: Intercepts traffic, adds token exchange headers via Lua filter, calls Ext Proc
- **Ext Proc**: Performs the actual token exchange with Keycloak
- **AuthProxy**: Validates incoming tokens

### Standalone Deployment

AuthProxy can also be deployed as a standalone service for validating incoming requests:

```
Client ──► AuthProxy (validates token) ──► Target Service
```

## Configuration

### AuthProxy Environment Variables

| Variable | Required | Description | Example |
|----------|----------|-------------|---------|
| `JWKS_URL` | Yes | JWKS endpoint for token validation | `http://keycloak:8080/realms/demo/.../certs` |
| `ISSUER` | Yes | Expected token issuer (must match `iss` claim) | `http://keycloak:8080/realms/demo` |
| `AUDIENCE` | No | Expected token audience (if empty, skips audience validation) | `my-service` |
| `TARGET_SERVICE_URL` | No | Downstream service URL | `http://target-service:8081` |
| `PORT` | No | Listen port (default: `8080`) | `8080` |

#### Transparent Mode (No Audience Validation)

When `AUDIENCE` is **not set**, AuthProxy operates in **transparent mode**:
- Validates only the token **signature** and **issuer**
- Accepts tokens with **any audience**
- Relies on **token exchange** (via Ext Proc) to set the correct target audience

This is useful when the caller obtains a token for itself and you want the proxy to transparently exchange it:

```
Caller gets token: aud=caller  →  AuthProxy (transparent)  →  Token Exchange  →  aud=target-service
```

To enable transparent mode, simply omit the `AUDIENCE` environment variable:

```yaml
env:
  - name: JWKS_URL
    value: "http://keycloak:8080/realms/demo/protocol/openid-connect/certs"
  - name: ISSUER
    value: "http://keycloak:8080/realms/demo"
  # AUDIENCE not set - transparent mode enabled
```

### Token Exchange Configuration

The Ext Proc receives token exchange parameters via internal headers (injected by Envoy Lua filter from environment variables):

| Header | Description | Source |
|--------|-------------|--------|
| `x-token-url` | Keycloak token endpoint URL | `TOKEN_URL` env var |
| `x-client-id` | Client ID for token exchange | `/shared/client-id.txt` or `CLIENT_ID` env var |
| `x-client-secret` | Client secret | `/shared/client-secret.txt` or `CLIENT_SECRET` env var |

#### Configuration Secret

Token exchange is configured via a Kubernetes Secret:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: auth-proxy-config
stringData:
  TOKEN_URL: "http://keycloak:8080/realms/demo/protocol/openid-connect/token"
  TARGET_AUDIENCE: "target-service"
  TARGET_SCOPES: "openid target-service-aud"
```

| Variable | Description | Example |
|----------|-------------|---------|
| `TOKEN_URL` | Keycloak token endpoint | `http://keycloak:8080/realms/demo/protocol/openid-connect/token` |
| `TARGET_AUDIENCE` | Target service audience | `auth-target` |
| `TARGET_SCOPES` | Scopes for exchanged token | `openid auth-target-aud` |

> **Note:** `CLIENT_ID` and `CLIENT_SECRET` can come from environment variables or from `/shared/` files (when using dynamic client registration with SPIFFE).

## Token Exchange Flow

The Ext Proc performs OAuth 2.0 Token Exchange as defined in [RFC 8693](https://datatracker.ietf.org/doc/html/rfc8693):

```
POST /realms/demo/protocol/openid-connect/token
Content-Type: application/x-www-form-urlencoded

grant_type=urn:ietf:params:oauth:grant-type:token-exchange
&client_id=<client-id>
&client_secret=<client-secret>
&subject_token=<original-jwt>
&subject_token_type=urn:ietf:params:oauth:token-type:access_token
&requested_token_type=urn:ietf:params:oauth:token-type:access_token
&audience=<target-audience>
&scope=<target-scopes>
```

**Response:**

```json
{
  "access_token": "<new-jwt-with-target-audience>",
  "token_type": "Bearer",
  "expires_in": 300
}
```

## Standalone Quickstart

This section provides complete instructions to run AuthProxy standalone, without the full AuthBridge setup (no SPIFFE, no client-registration).

### Prerequisites

- Kubernetes cluster (Kind recommended)
- Keycloak deployed (or use [Kagenti installer](https://github.com/kagenti/kagenti/blob/main/docs/install.md))
- Docker/Podman for building images

### Step 1: Build and Deploy

```bash
cd AuthBridge/AuthProxy

# Build all images
make build-images

# Load into Kind cluster (set KIND_CLUSTER_NAME if not using default)
make load-images

# Deploy auth-proxy and demo-app
make deploy
```

This deploys:
- `auth-proxy` - JWT validation proxy (port 8080)
- `demo-app` - Sample target application (port 8081)

### Step 2: Configure Keycloak

Port-forward Keycloak (in a separate terminal):

```bash
kubectl port-forward service/keycloak-service -n keycloak 8080:8080
```

Run the setup script to create necessary Keycloak clients:

```bash
cd quickstart

# Setup Python environment
python -m venv venv
source venv/bin/activate
pip install -r requirements.txt

# Configure Keycloak
python setup_keycloak.py
```

The script creates:
- `application-caller` client - for obtaining tokens
- `auth_proxy` client - for token exchange
- `demo-app` client - target audience
- A test user (`test-user` / `password`)

**Copy the exported `CLIENT_SECRET` from the script output.**

### Step 3: Test the Flow

Port-forward AuthProxy (in a separate terminal):

```bash
kubectl port-forward svc/auth-proxy-service 9090:8080
```

Get a token and test:

```bash
# Export the CLIENT_SECRET from Step 2
export CLIENT_SECRET="<from-setup-script>"

# Get an access token
export ACCESS_TOKEN=$(curl -sX POST \
  "http://keycloak.localtest.me:8080/realms/demo/protocol/openid-connect/token" \
  -d "grant_type=password" \
  -d "client_id=application-caller" \
  -d "client_secret=$CLIENT_SECRET" \
  -d "username=test-user" \
  -d "password=password" | jq -r '.access_token')

# Valid request (will be forwarded to demo-app)
curl -H "Authorization: Bearer $ACCESS_TOKEN" http://localhost:9090/test
# Expected: "authorized"

# Invalid token (will be rejected)
curl -H "Authorization: Bearer invalid-token" http://localhost:9090/test
# Expected: "Unauthorized - invalid token"

# No token (will be rejected)
curl http://localhost:9090/test
# Expected: "Unauthorized - invalid token"
```

### View Logs

```bash
# Auth proxy logs
kubectl logs deployment/auth-proxy

# Demo app logs
kubectl logs deployment/demo-app

# Follow logs in real-time
kubectl logs -f deployment/auth-proxy
```

### Clean Up

```bash
# Remove deployments
make undeploy

# Delete Kind cluster (if desired)
make kind-delete
```

> **📘 For detailed standalone instructions**, see the [Quickstart Guide](./quickstart/README.md).

---

## Testing (When Deployed as Sidecar)

When AuthProxy is deployed as a sidecar (e.g., in the AuthBridge demo):

```bash
# AuthProxy logs (shows token validation)
kubectl logs <pod-name> -c auth-proxy

# Envoy/Ext Proc logs (shows token exchange)
kubectl logs <pod-name> -c envoy-proxy
```

## Related Documentation

- [AuthBridge](../README.md) - Complete AuthBridge overview with token exchange flow
- [AuthBridge Demo](../demo.md) - Step-by-step demo instructions
- [Client Registration](../client-registration/README.md) - Automatic Keycloak client registration with SPIFFE
- [OAuth 2.0 Token Exchange (RFC 8693)](https://datatracker.ietf.org/doc/html/rfc8693)
- [Envoy External Processing](https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_filters/ext_proc_filter)
