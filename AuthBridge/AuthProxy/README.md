# AuthProxy

AuthProxy is a **JWT validation and token exchange proxy** for Kubernetes workloads. It enables secure service-to-service communication by intercepting and validating incoming tokens and transparently exchanging them for tokens with the correct audience for downstream services.

## What AuthProxy Does

AuthProxy solves a common challenge in microservices architectures: **how can a service call another service when each service expects tokens with different audiences?**

### The Problem

When a Caller obtains a token from another service, the token is scoped to a specific audience, most likely for itself.
If the Caller wants to pass the same token to a different service, the request will be rejected, since the service would expect
a different audience.

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
│   Caller    │ ── Token A ──►│       AuthProxy          │- Token B ──► │   Target    │  ✅ AUTHORIZED
│             │               │  1. Validate token       │              │             │
│ Token:      │               │  2. Exchange for new aud │              │ (expects    │
│ (aud: svc-a)│               |  3. Forward request      │              │ aud: target)│
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
- Checks token claims: `issuer`, `audience`, and optionally `scope`
- Forwards validated requests to the target service
- Returns `401 Unauthorized` for invalid tokens

**Configuration via environment variables:**
| Variable | Required | Description | Example |
|----------|----------|-------------|---------|
| `JWKS_URL` | Yes | URL to fetch public keys for JWT validation | `http://keycloak:8080/realms/demo/.../certs` |
| `ISSUER` | Yes | Expected token issuer | `http://keycloak:8080/realms/demo` |
| `AUDIENCE` | No | Expected token audience (if empty, accepts any valid token) | `agent` |
| `TARGET_SERVICE_URL` | No | URL to forward requests to | `http://target-service:8081` |

#### Transparent Mode (No Audience Validation)

When `AUDIENCE` is **not set**, AuthProxy operates in **transparent mode**:
- Validates only the token **signature** and **issuer**
- Accepts tokens with **any audience** (e.g., the caller's SPIFFE ID)
- Relies on **token exchange** (via Ext Proc) to set the correct target audience

This is useful when the caller obtains a token for itself and you want the proxy to transparently exchange it:

```
Caller gets token: aud=caller  →  AuthProxy (transparent)  →  Token Exchange  →  aud=auth-target
```

To enable transparent mode, simply omit the `AUDIENCE` environment variable:

```yaml
env:
  - name: JWKS_URL
    value: "http://keycloak:8080/realms/demo/protocol/openid-connect/certs"
  - name: ISSUER
    value: "http://keycloak:8080/realms/demo"
  # AUDIENCE not set - transparent mode
```

### 2. Ext Proc - External Processor (`go-processor/main.go`)

An Envoy external processor (gRPC) that:
- Runs on port **9090**
- Intercepts HTTP requests via Envoy
- Performs **OAuth 2.0 Token Exchange** ([RFC 8693](https://datatracker.ietf.org/doc/html/rfc8693))
- Replaces the `Authorization` header with the exchanged token
- Works transparently - the caller doesn't know token exchange happened

**Token Exchange Parameters (via headers from Envoy Lua filter):**
| Header | Description |
|--------|-------------|
| `x-token-url` | Token endpoint URL |
| `x-client-id` | Client ID for token exchange |
| `x-client-secret` | Client secret |
| `x-target-audience` | Desired audience for new token |
| `x-target-scopes` | Desired scopes for new token |

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
- **AuthProxy**: Validates incoming tokens (can also be used for inbound traffic)

### Standalone Deployment

AuthProxy can also be deployed as a standalone service for validating incoming requests:

```
Client ──► AuthProxy (validates token) ──► Target Service
```

## Quick Start

### Prerequisites

- Kubernetes cluster (Kind recommended)
- Keycloak deployed and configured
- Docker/Podman for building images

### Build Images

```bash
cd AuthBridge/AuthProxy

# Build all images
make build-images

# Load into Kind cluster
make load-images
```

This builds:

- `auth-proxy:latest` - JWT validation proxy
- `demo-app:latest` - Sample target application
- `proxy-init:latest` - iptables init container
- `envoy-with-processor:latest` - Envoy + Ext Proc

### Deploy

See the [AuthBridge Demo](../README.md) for complete deployment instructions with SPIFFE integration.

For standalone auth-proxy deployment:

```bash
# Deploy auth-proxy and demo-app
make deploy

# Port forward to test
kubectl port-forward svc/auth-proxy-service 8080:8080
```

## Testing

### Test with a Valid Token

```bash
# Get a token from Keycloak
TOKEN=$(curl -s http://keycloak:8080/realms/demo/protocol/openid-connect/token \
  -d 'grant_type=client_credentials' \
  -d 'client_id=my-client' \
  -d 'client_secret=my-secret' | jq -r '.access_token')

# Call through auth-proxy
curl -H "Authorization: Bearer $TOKEN" http://localhost:8080/test
# Expected: "authorized"
```

### Test with Invalid Token

```bash
curl -H "Authorization: Bearer invalid-token" http://localhost:8080/test
# Expected: 401 Unauthorized - Invalid token
```

### View Logs

```bash
# AuthProxy logs (shows token validation)
kubectl logs deployment/caller -c auth-proxy

# Envoy/Ext Proc logs (shows token exchange)
kubectl logs deployment/caller -c envoy-proxy

# Target app logs (shows received token)
kubectl logs deployment/auth-target
```

## Token Exchange Flow

The Ext Proc performs OAuth 2.0 Token Exchange as defined in [RFC 8693](https://datatracker.ietf.org/doc/html/rfc8693):

```cmd
POST /realms/demo/protocol/openid-connect/token
Content-Type: application/x-www-form-urlencoded

grant_type=urn:ietf:params:oauth:grant-type:token-exchange
&client_id=agent
&client_secret=<client-secret>
&subject_token=<original-jwt>
&subject_token_type=urn:ietf:params:oauth:token-type:access_token
&requested_token_type=urn:ietf:params:oauth:token-type:access_token
&audience=auth-target
&scope=openid auth-target-aud
```

**Response:**

```json
{
  "access_token": "<new-jwt-with-auth-target-audience>",
  "token_type": "Bearer",
  "expires_in": 300
}
```

## Configuration

### AuthProxy Environment Variables

| Variable | Required | Description |
|----------|----------|-------------|
| `JWKS_URL` | Yes | JWKS endpoint for token validation |
| `ISSUER` | Yes | Expected token issuer (must match `iss` claim) |
| `AUDIENCE` | Yes | Expected token audience (must match `aud` claim) |
| `TARGET_SERVICE_URL` | No | Downstream service URL (default: `http://demo-app-service:8081`) |
| `PORT` | No | Listen port (default: `8080`) |

### Token Exchange Configuration (via Kubernetes Secret)

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: auth-proxy-config
stringData:
  TOKEN_URL: "http://keycloak:8080/realms/demo/protocol/openid-connect/token"
  CLIENT_ID: "agent"
  CLIENT_SECRET: "your-secret"
  TARGET_AUDIENCE: "auth-target"
  TARGET_SCOPES: "openid auth-target-aud"
```

## Clean Up

```bash
# Remove deployments
make undeploy

# Delete Kind cluster (if using Kind)
make kind-delete
```

## Related Documentation

- [AuthBridge Demo](../README.md) - Complete end-to-end demo with SPIFFE
- [Client Registration](../client-registration/README.md) - Automatic Keycloak client registration
- [OAuth 2.0 Token Exchange (RFC 8693)](https://datatracker.ietf.org/doc/html/rfc8693)
- [Envoy External Processing](https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_filters/ext_proc_filter)
