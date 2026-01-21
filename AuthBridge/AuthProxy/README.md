# AuthProxy

AuthProxy is a **token exchange sidecar** for Kubernetes workloads. It enables secure service-to-service communication by intercepting outgoing requests and transparently exchanging tokens for ones with the correct audience for downstream services.

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

AuthProxy intercepts outgoing requests and exchanges the token for a new one with the correct audience:

```
┌─────────────┐               ┌──────────────────────────┐              ┌─────────────┐
│   Caller    │ ── Token A ──►│       AuthProxy          │─ Token B ──► │   Target    │  ✅ AUTHORIZED
│             │               │  1. Intercept request    │              │             │
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

AuthProxy is a **single sidecar container** that consists of:

### Envoy Proxy with Ext Proc Filter

The sidecar runs an Envoy proxy with an external processor (ext-proc) filter that:
- **Envoy Proxy** (port **15123**): Intercepts all outbound HTTP traffic from the application container
- **Ext Proc Filter** (`go-processor/main.go`, port **9090**): Performs **OAuth 2.0 Token Exchange** ([RFC 8693](https://datatracker.ietf.org/doc/html/rfc8693))
  - Intercepts HTTP requests via Envoy
  - Replaces the `Authorization` header with the exchanged token
  - Works transparently—the caller doesn't know token exchange happened

### Traffic Interception via iptables

To automatically route traffic from the main application container to the AuthProxy sidecar, an **init container** (`proxy-init`) configures **iptables rules** to redirect all **OUTBOUND** network packets to Envoy. This ensures transparent interception without requiring any changes to the application code.

### Example Application (`main.go`)

The `main.go` file in this directory is **not** a core component of AuthProxy. It is an **example application** that demonstrates how to use the AuthProxy sidecar. Any application can benefit from AuthProxy simply by being deployed alongside the sidecar—no code changes required.

## Architecture

### Sidecar Deployment

When deployed as a sidecar, AuthProxy intercepts all **outbound** traffic from the application via iptables:

```
┌────────────────────────────────────────────────────────────────┐
│                             POD                                │
│                                                                │
│  ┌─────────────┐       ┌────────────────────────────────────┐  │
│  │ proxy-init  │       │        AuthProxy Sidecar           │  │
│  │ (iptables)  │       │                                    │  │
│  └──────┬──────┘       │  ┌────────────┐    ┌────────────┐  │  │
│         │              │  │   Envoy    │◄──►│  Ext Proc  │  │  │
│         │              │  │   :15123   │    │   :9090    │  │  │
│         ▼              │  └─────┬──────┘    └──────┬─────┘  │  │
│  ┌─────────────┐       │        │                  │        │  │
│  │             │       │        │                  │        │  │
│  │ Application ├───────┼───────►│                  │        │  │
│  │  (any app)  │       │        │                  │        │  │
│  └─────────────┘       └────────┼──────────────────┼────────┘  │
│                                 │                  │           │
└─────────────────────────────────┼──────────────────┼───────────┘
                                  │                  │
                                  ▼                  ▼
                        ┌──────────────────┐  ┌ ─ ─ ─ ─ ─ ─ ─ ─ ┐
                        │  Target Service  │      Keycloak
                        └──────────────────┘  │(token exchange) │
                                               ─ ─ ─ ─ ─ ─ ─ ─ ─
```

**How it works:**
1. **proxy-init** (init container): Sets up iptables rules on the application to redirect all outbound traffic to Envoy
2. **Envoy** (port 15123): Intercepts redirected traffic, calls Ext Proc via gRPC
3. **Ext Proc** (port 9090): Performs OAuth 2.0 token exchange with Keycloak, returns modified headers to Envoy
4. **Envoy**: Applies the new Authorization header and forwards the request to the target service

The application requires **no code changes**—traffic interception is completely transparent.

## Configuration

### Token Exchange Configuration (AuthProxy Sidecar)

The Ext Proc reads token exchange configuration directly from environment variables at startup:

| Variable | Description | Source |
|----------|-------------|--------|
| `TOKEN_URL` | Keycloak token endpoint URL | Environment variable |
| `CLIENT_ID` | Client ID for token exchange | `/shared/client-id.txt` file or `CLIENT_ID` env var |
| `CLIENT_SECRET` | Client secret | `/shared/client-secret.txt` file or `CLIENT_SECRET` env var |
| `TARGET_AUDIENCE` | Target service audience | Environment variable |
| `TARGET_SCOPES` | Scopes for exchanged token | Environment variable |

> **Note:** `CLIENT_ID` and `CLIENT_SECRET` are preferentially loaded from `/shared/` files (when using dynamic client registration with SPIFFE). If files are not available, environment variables are used as fallback.

#### Configuration Secret

Token exchange is typically configured via a Kubernetes Secret:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: auth-proxy-config
stringData:
  TOKEN_URL: "http://keycloak:8080/realms/demo/protocol/openid-connect/token"
  CLIENT_ID: "auth_proxy"
  CLIENT_SECRET: "<your-client-secret>"
  TARGET_AUDIENCE: "target-service"
  TARGET_SCOPES: "openid target-service-aud"
```

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

## Quickstart

This section provides instructions to run the example application with the AuthProxy sidecar, without the full AuthBridge setup (no SPIFFE, no client-registration).

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

# Deploy example app with AuthProxy sidecar
make deploy
```

This deploys:
- `auth-proxy` - Example application with JWT validation (port 8080) and the AuthProxy sidecar (Envoy + Ext Proc)
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

Port-forward the example application service (in a separate terminal):

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
# Example application logs
kubectl logs deployment/auth-proxy

# Demo app (target service) logs
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

## Viewing Logs

When running the example deployment:

```bash
# Example application logs
kubectl logs <pod-name> -c auth-proxy

# AuthProxy sidecar logs (shows token exchange)
kubectl logs <pod-name> -c envoy-proxy
```

## Related Documentation

- [AuthBridge](../README.md) - Complete AuthBridge overview with token exchange flow
- [AuthBridge Demo](../demo.md) - Step-by-step demo instructions
- [Client Registration](../client-registration/README.md) - Automatic Keycloak client registration with SPIFFE
- [OAuth 2.0 Token Exchange (RFC 8693)](https://datatracker.ietf.org/doc/html/rfc8693)
- [Envoy External Processing](https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_filters/ext_proc_filter)
