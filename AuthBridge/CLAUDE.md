# CLAUDE.md - AuthBridge

This file provides context for Claude (AI assistant) when working with the `AuthBridge` codebase.
For the full monorepo context (webhook, CI/CD, Helm, cross-component relationships), see [`../CLAUDE.md`](../CLAUDE.md).
For the webhook internals, see [`../kagenti-webhook/CLAUDE.md`](../kagenti-webhook/CLAUDE.md).

## What AuthBridge Does

AuthBridge provides **zero-trust, transparent token management** for Kubernetes workloads. It combines three capabilities:

1. **Automatic Identity** -- Workloads obtain SPIFFE IDs from SPIRE and auto-register as Keycloak clients
2. **Inbound JWT Validation** -- Incoming requests are validated (signature, issuer, audience) by an Envoy ext-proc
3. **Outbound Token Exchange** -- Outgoing requests get their tokens automatically exchanged for the correct target audience (OAuth 2.0 RFC 8693)

All of this happens transparently via sidecar injection -- no application code changes required.

## Directory Structure

```
AuthBridge/
├── AuthProxy/                        # Envoy + ext-proc sidecar (Go)
│   ├── go-processor/
│   │   ├── main.go                   #   gRPC ext-proc: inbound validation + outbound token exchange
│   │   └── internal/                 #   Internal packages
│   ├── main.go                       #   Example pass-through proxy app (NOT a core component)
│   ├── init-iptables.sh              #   iptables setup (outbound + inbound, Istio ambient compatible)
│   ├── entrypoint-envoy.sh           #   Starts go-processor + Envoy
│   ├── Dockerfile                    #   auth-proxy example app image
│   ├── Dockerfile.envoy              #   envoy-with-processor (Envoy 1.28 + go-processor)
│   ├── Dockerfile.init               #   proxy-init (Alpine + iptables)
│   ├── Makefile                      #   Build/deploy targets for quickstart
│   ├── go.mod                        #   Go module (github.com/kagenti/kagenti-extensions/AuthBridge/AuthProxy)
│   ├── README.md                     #   AuthProxy overview
│   ├── k8s/                          #   Standalone K8s manifests for AuthProxy
│   │   ├── auth-proxy-deployment.yaml
│   │   └── go-processor-deployment.yaml
│   └── quickstart/                   #   Standalone quickstart (no SPIFFE)
│       ├── README.md                 #     Step-by-step tutorial
│       ├── setup_keycloak.py         #     Creates Keycloak clients/scopes for quickstart demo
│       ├── requirements.txt          #     python-keycloak==5.3.1
│       ├── demo-app/
│       │   ├── main.go               #     Target service: JWT validation on :8081, TLS echo on :8443
│       │   └── Dockerfile
│       └── k8s/
│           └── demo-app-deployment.yaml
│
├── client-registration/              # Keycloak auto-registration (Python)
│   ├── client_registration.py        #   Main script: register client, write secret
│   ├── Dockerfile                    #   Python 3.12-slim, UID/GID 1000
│   ├── requirements.txt              #   python-keycloak==5.3.1, pyjwt==2.10.1
│   ├── README.md                     #   Detailed docs with SPIFFE/non-SPIFFE examples
│   ├── example_deployment.yaml       #   Example without SPIFFE
│   ├── example_deployment_spiffe.yaml#   Example with SPIFFE
│   └── images/                       #   Architecture diagrams
│
├── demos/                            # Demo scenarios with full setup
│   ├── single-target/                #   Single agent → single target demo (SPIFFE-based)
│   │   ├── demo.md                   #     Full walkthrough (manual + SPIFFE)
│   │   ├── setup_keycloak.py         #     Creates auth-target client, scopes, alice user
│   │   └── k8s/                      #     K8s manifests
│   │       ├── authbridge-deployment.yaml
│   │       ├── authbridge-deployment-no-spiffe.yaml
│   │       ├── agent-deployment-webhook.yaml
│   │       ├── agent-deployment-webhook-no-spiffe.yaml
│   │       ├── auth-target-deployment-webhook.yaml
│   │       └── configmaps-webhook.yaml
│   ├── multi-target/                 #   Multi-target demo with keycloak_sync
│   │   ├── demo.md                   #     Walkthrough for multiple targets
│   │   └── k8s/                      #     K8s manifests
│   │       ├── alice-deployment.yaml
│   │       ├── bob-deployment.yaml
│   │       └── configmaps.yaml
│   └── github-issue/                 #   GitHub issue integration demo
│       ├── demo.md                   #     Automated demo walkthrough
│       ├── demo-manual.md            #     Manual demo walkthrough
│       ├── setup_keycloak.py         #     Creates github-tool client, scopes, users
│       └── k8s/                      #     K8s manifests
│           ├── github-tool-deployment.yaml
│           └── configmaps.yaml
│
├── keycloak_sync.py                  # Declarative Keycloak sync tool (routes.yaml driven)
├── demo-webhook.md                   # Demo walkthrough for webhook-based injection
├── setup_keycloak-webhook.py         # Keycloak setup for webhook-injected deployments
├── README.md                         # AuthBridge overview and architecture
└── requirements.txt                  # python-keycloak==5.3.1
```

## Component Details

### AuthProxy (go-processor/main.go)

The core ext-proc that handles both traffic directions:

**Inbound path** (`x-authbridge-direction: inbound`):
- Validates JWT signature via JWKS (auto-refreshing cache from `TOKEN_URL`-derived JWKS endpoint)
- Validates issuer claim against `ISSUER` env var
- Optionally validates audience against `EXPECTED_AUDIENCE` env var
- Returns 401 with JSON error body for invalid/missing tokens
- Removes `x-authbridge-direction` header before forwarding to app

**Outbound path** (no direction header):
- Reads `Authorization: Bearer <token>` from request
- Performs OAuth 2.0 Token Exchange (RFC 8693) against Keycloak
- Replaces Authorization header with the exchanged token
- If config is incomplete or exchange fails, passes request through unchanged

**Configuration loading:**
- Waits up to 60s for credential files from client-registration (`waitForCredentials`)
- Reads `CLIENT_ID` from `/shared/client-id.txt` (file) or `CLIENT_ID` env var (fallback)
- Reads `CLIENT_SECRET` from `/shared/client-secret.txt` (file) or `CLIENT_SECRET` env var (fallback)
- Static config from env vars: `TOKEN_URL`, `ISSUER`, `EXPECTED_AUDIENCE`, `TARGET_AUDIENCE`, `TARGET_SCOPES`
- JWKS URL is derived from TOKEN_URL: replaces `/token` suffix with `/certs`

**Key types:**
- `Config` struct -- holds client credentials and token exchange params (thread-safe via `sync.RWMutex`)
- `processor` struct -- implements `ExternalProcessorServer` gRPC interface
- `tokenExchangeResponse` -- JSON response from Keycloak token endpoint

### init-iptables.sh

Extensively documented shell script that sets up iptables for transparent traffic interception. Key features:

- **Outbound**: `PROXY_OUTPUT` chain in `nat OUTPUT`, redirects to Envoy port 15123
- **Inbound**: `PROXY_INBOUND` chain in `nat PREROUTING`, redirects to Envoy port 15124
- **Istio ambient mesh coexistence**: Handles ztunnel fwmark (0x539), HBONE port (15008), route_localnet sysctl
- **Exclusions**: SSH (22), loopback, configurable `OUTBOUND_PORTS_EXCLUDE` and `INBOUND_PORTS_EXCLUDE`
- **Envoy UID 1337**: Excluded from outbound redirect to prevent loops
- **Mangle rule**: Sets fwmark on Envoy's local delivery to prevent ISTIO_OUTPUT redirect loop
- Uses `-I 1` (insert first) for chain ordering stability with Istio CNI

**Environment variables:**
| Variable | Default | Description |
|----------|---------|-------------|
| `PROXY_PORT` | 15123 | Envoy outbound listener |
| `INBOUND_PROXY_PORT` | 15124 | Envoy inbound listener |
| `PROXY_UID` | 1337 | Envoy process UID (excluded from redirect) |
| `OUTBOUND_PORTS_EXCLUDE` | (empty) | Comma-separated ports to exclude |
| `INBOUND_PORTS_EXCLUDE` | (empty) | Comma-separated ports to exclude |

### client_registration.py

Idempotent Python script that:
1. Reads SPIFFE ID from `/opt/jwt_svid.token` JWT `sub` claim (if `SPIRE_ENABLED=true`)
2. Falls back to `CLIENT_NAME` env var as client ID (if SPIRE disabled)
3. Creates or reuses a Keycloak client with token exchange enabled
4. Retrieves the client secret and writes to `SECRET_FILE_PATH` (in cluster deployments, the webhook sets `SECRET_FILE_PATH=/shared/client-secret.txt` to match the shared-volume contract)

**Keycloak client configuration created:**
- `publicClient: False` (confidential/authenticated)
- `serviceAccountsEnabled: True` (allows `client_credentials` grant)
- `standardFlowEnabled: True`
- `directAccessGrantsEnabled: True`
- `standard.token.exchange.enabled: True`

**Dependencies:** `python-keycloak==5.3.1`, `pyjwt==2.10.1`

### keycloak_sync.py

A declarative Keycloak synchronization tool that maintains Keycloak client scope mappings based on a YAML configuration file (`routes.yaml`).

**Key features:**
- Reads `routes.yaml` to determine which client needs which scopes
- Idempotent: only makes changes when state differs from desired config
- Uses helper functions from setup scripts for client/scope operations
- Commonly used in multi-target demos where agents need dynamic scope assignments

**Configuration format (routes.yaml):**
```yaml
routes:
  - client: agent-a
    scopes:
      - target-service-aud
      - another-scope-aud
```

### Envoy Configuration (demos/single-target/k8s/configmaps-webhook.yaml)

The `envoy-config` ConfigMap contains the full Envoy YAML with:

**Listeners:**
- `outbound_listener` (port 15123):
  - `tls_inspector` + `original_dst` listener filters
  - TLS filter chain: `tcp_proxy` passthrough to `original_destination` cluster
  - Raw buffer filter chain: `http_connection_manager` with ext_proc + router
- `inbound_listener` (port 15124):
  - `original_dst` listener filter
  - Injects `x-authbridge-direction: inbound` request header
  - `http_connection_manager` with ext_proc + router

**Clusters:**
- `original_destination` -- ORIGINAL_DST type (routes to original IP/port)
- `ext_proc_cluster` -- STATIC, points to localhost:9090 (go-processor), HTTP/2

### Demo App (quickstart/demo-app/main.go)

A test target service with two servers:
- HTTP on `:8081` -- Validates JWT (issuer, audience, signature via JWKS)
- HTTPS on `:8443` -- Simple echo (`tls-ok`), no JWT validation, self-signed cert

Used for testing both the token exchange (HTTP) and TLS passthrough (HTTPS) paths.

## Demo Scenarios

The `demos/` directory contains three complete demonstration scenarios, each with its own setup scripts, K8s manifests, and walkthrough documentation:

### demos/single-target/
The primary demo showing agent → target communication with SPIFFE identity and token exchange. This is the recommended starting point for understanding AuthBridge.

**Contents:**
- `demo.md` -- Full walkthrough with manual and SPIFFE-based flows
- `setup_keycloak.py` -- Creates `auth-target` client, `agent-spiffe-aud` + `auth-target-aud` scopes, `alice` user
- `k8s/` -- All manifests including webhook-based deployments and configmaps

**Key concepts demonstrated:**
- SPIFFE ID-based client registration
- Inbound JWT validation
- Outbound token exchange (RFC 8693)
- Webhook-based sidecar injection

### demos/multi-target/
Demonstrates dynamic scope assignment for agents communicating with multiple targets using `keycloak_sync.py`.

**Contents:**
- `demo.md` -- Walkthrough for multi-target scenarios
- `k8s/` -- Manifests for multiple agent/target deployments (alice, bob)

**Key concepts demonstrated:**
- Dynamic scope management via `keycloak_sync.py` + `routes.yaml`
- Multiple agents with different access patterns
- Declarative Keycloak state management

### demos/github-issue/
Shows integration with external APIs (GitHub) using AuthBridge for transparent authentication.

**Contents:**
- `demo.md` -- Automated demo walkthrough
- `demo-manual.md` -- Manual step-by-step instructions
- `setup_keycloak.py` -- Creates `github-tool` client, `github-tool-aud` + `github-full-access` scopes, `alice` + `bob` users
- `k8s/` -- Manifests for GitHub integration demo

**Key concepts demonstrated:**
- External API integration via token exchange
- Service-to-service authentication patterns
- Multi-user scenarios (alice, bob)

## Keycloak Setup Scripts

There are **four** setup scripts for different demo scenarios:

| Script | Location | Use Case |
|--------|----------|----------|
| `setup_keycloak.py` | `AuthBridge/demos/single-target/` | Single-target SPIFFE demo (creates realm, auth-target client, agent-spiffe-aud + auth-target-aud scopes, alice user) |
| `setup_keycloak.py` | `AuthBridge/demos/github-issue/` | GitHub issue integration demo (creates github-tool client, github-tool-aud + github-full-access scopes, alice + bob users) |
| `setup_keycloak-webhook.py` | `AuthBridge/` | Webhook-injected deployments (parameterized namespace/SA, creates same resources as single-target with dynamic SPIFFE ID) |
| `setup_keycloak.py` | `AuthBridge/AuthProxy/quickstart/` | Standalone AuthProxy quickstart without SPIFFE (creates application-caller, authproxy, demoapp clients with per-client scope assignment) |

**Common Keycloak defaults across all scripts:**
- URL: `http://keycloak.localtest.me:8080`
- Realm: `demo`
- Admin: `admin` / `admin`

**Note:** All scripts share the same helper function patterns (`get_or_create_realm`, `get_or_create_client`, `get_or_create_client_scope`, etc.) and are idempotent.

## Required ConfigMaps for Webhook Injection

When the kagenti-webhook injects sidecars, four ConfigMaps must exist in the target namespace. All are defined in `demos/single-target/k8s/configmaps-webhook.yaml`:

| ConfigMap | Consumer | Key Fields |
|-----------|----------|------------|
| `environments` | client-registration | `KEYCLOAK_URL`, `KEYCLOAK_REALM`, `KEYCLOAK_ADMIN_USERNAME`, `KEYCLOAK_ADMIN_PASSWORD`, `SPIRE_ENABLED` |
| `authbridge-config` | envoy-proxy (ext-proc) | `TOKEN_URL`, `ISSUER`, `TARGET_AUDIENCE`, `TARGET_SCOPES` |
| `spiffe-helper-config` | spiffe-helper | `helper.conf` (SPIRE agent address, cert paths, JWT SVID config) |
| `envoy-config` | envoy-proxy | `envoy.yaml` (full Envoy configuration) |

## Shared Volume Contract

Sidecars communicate through files on shared volumes:

| Path | Writer | Reader | Content |
|------|--------|--------|---------|
| `/opt/jwt_svid.token` | spiffe-helper | client-registration | JWT SVID from SPIRE |
| `/shared/client-id.txt` | client-registration | envoy-proxy (ext-proc) | SPIFFE ID or CLIENT_NAME |
| `/shared/client-secret.txt` | client-registration | envoy-proxy (ext-proc) | Keycloak client secret |

## Build and Deploy

### AuthProxy (standalone quickstart, no webhook)

```bash
cd AuthBridge/AuthProxy

# Build all images (auth-proxy, demo-app, proxy-init, envoy-with-processor)
make build-images

# Load into Kind cluster
make load-images                    # Uses KIND_CLUSTER_NAME env var (default: kagenti)

# Deploy auth-proxy + demo-app
make deploy

# Clean up
make undeploy
```

### Full Demo with Webhook (Single Target)

```bash
# 1. Setup Keycloak (requires port-forward to Keycloak)
cd AuthBridge/demos/single-target
pip install -r ../../requirements.txt
python setup_keycloak.py            # Creates realm, auth-target client, scopes, alice user

# 2. Apply ConfigMaps to target namespace
kubectl apply -f k8s/configmaps-webhook.yaml -n <namespace>

# 3. Deploy workloads (webhook auto-injects sidecars)
kubectl apply -f k8s/authbridge-deployment.yaml           # With SPIFFE
# or
kubectl apply -f k8s/authbridge-deployment-no-spiffe.yaml # Without SPIFFE
```

## Important Port Mapping

| Port | Component | Protocol | Purpose |
|------|-----------|----------|---------|
| 15123 | Envoy | TCP | Outbound listener (iptables redirects app traffic here) |
| 15124 | Envoy | TCP | Inbound listener (iptables redirects incoming traffic here) |
| 9090 | go-processor | gRPC | Ext-proc server (called by Envoy) |
| 9901 | Envoy | HTTP | Admin interface (bound to 127.0.0.1) |
| 8080 | auth-proxy | HTTP | Example app (NOT part of sidecar) |
| 8081 | demo-app | HTTP | Demo target (JWT validation) |
| 8443 | demo-app | HTTPS | Demo target (TLS echo, no JWT) |

## Code Conventions

### Go (AuthProxy, go-processor, demo-app)
- Go 1.23 (module: `github.com/kagenti/kagenti-extensions/AuthBridge/AuthProxy`)
- Logging with `log.Printf` (stdlib), prefixed by `[Config]`, `[Token Exchange]`, `[Inbound]`, `[JWT Debug]`
- Thread-safe config via `sync.RWMutex` in the `Config` struct
- gRPC ext-proc using `envoyproxy/go-control-plane` types
- JWT validation with `lestrrat-go/jwx/v2`

### Python (client-registration, setup scripts)
- Python 3.12 syntax (type hints: `str | None`)
- `python-keycloak` library for all Keycloak admin API calls
- `PyJWT` for JWT decoding (signature verification disabled -- uses `verify_signature: False`)
- Idempotent: all `get_or_create_*` helper functions check existence before creating
- UID/GID 1000 in Dockerfile **must match** `ClientRegistrationUID`/`ClientRegistrationGID` in `kagenti-webhook/internal/webhook/injector/container_builder.go`

### Shell (init-iptables.sh)
- `set -e` (exit on error)
- Extensive inline documentation explaining iptables chain ordering, Istio interactions, and debugging tips
- Idempotent: uses `iptables -N ... 2>/dev/null || true` and `iptables -F` before adding rules

## Common Tasks for Code Changes

### Modifying Token Exchange Logic
- Edit `go-processor/main.go`, function `exchangeToken()`
- The token exchange POST parameters follow RFC 8693 exactly
- Test by rebuilding: `make docker-build-envoy && make load-images`

### Modifying Inbound JWT Validation
- Edit `go-processor/main.go`, functions `validateInboundJWT()` and `handleInbound()`
- JWKS cache is initialized in `initJWKSCache()` and auto-refreshes
- Direction detection: `x-authbridge-direction: inbound` header (injected by Envoy inbound listener config)

### Adding New iptables Rules
- Edit `init-iptables.sh`
- Follow the existing pattern: document the rule's purpose, Istio interaction, and chain ordering
- Test with and without Istio ambient mesh if possible
- Rebuild: `make docker-build-init && make load-images`

### Modifying Client Registration
- Edit `client-registration/client_registration.py`
- The `register_client()` function is idempotent
- Keycloak client payload is the main configuration point
- Test: `kubectl delete pod <pod> -n <ns>` to trigger re-registration

### Adding New Keycloak Resources to Setup
- Edit the appropriate `setup_keycloak*.py` script
- Use the `get_or_create_*` helper pattern for idempotency
- All scripts use `python-keycloak` library (KeycloakAdmin class)

### Changing Envoy Configuration
- Edit the `envoy.yaml` section in `demos/single-target/k8s/configmaps-webhook.yaml` (or the appropriate demo's configmaps file)
- Key listener/cluster names: `outbound_listener`, `inbound_listener`, `original_destination`, `ext_proc_cluster`
- After changes, re-apply the ConfigMap and restart pods

## Gotchas and Known Issues

1. **Credential file race condition**: The ext-proc waits up to 60s for `/shared/client-id.txt` and `/shared/client-secret.txt`. If client-registration takes longer (e.g., Keycloak slow to start), the ext-proc will fall back to env vars which may be empty.

2. **ISSUER vs TOKEN_URL**: `ISSUER` must be the Keycloak **frontend URL** (what appears in the `iss` claim of tokens), while `TOKEN_URL` is the **internal service URL**. These are often different in Kubernetes (e.g., `http://keycloak.localtest.me:8080` vs `http://keycloak-service.keycloak.svc:8080`).

3. **Keycloak port exclusion**: When using iptables interception, Keycloak's port (8080) must be excluded from redirect via `OUTBOUND_PORTS_EXCLUDE=8080`. Otherwise, token exchange requests from the ext-proc get redirected back to Envoy, creating a loop.

4. **TLS passthrough is one-way**: Outbound HTTPS traffic passes through Envoy without token exchange. There is no mechanism to exchange tokens for HTTPS destinations. Only HTTP outbound traffic gets token exchange.

5. **Virtualenv directory**: For local development you may create `AuthProxy/quickstart/venv/`, but it should be gitignored and is not committed to the repo.

6. **Demo SPIFFE ID is hardcoded**: `demos/single-target/setup_keycloak.py` hardcodes `AGENT_SPIFFE_ID = "spiffe://localtest.me/ns/authbridge/sa/agent"`. Change this if using a different namespace/SA.

7. **Admin credentials in ConfigMap**: `demos/single-target/k8s/configmaps-webhook.yaml` stores Keycloak admin credentials in a ConfigMap (not a Secret). This is for demo only -- production should use Kubernetes Secrets.

8. **Envoy Lua filter required for inbound**: The `x-authbridge-direction: inbound` header MUST be injected via a Lua filter before ext_proc in the inbound listener. Route-level `request_headers_to_add` does NOT work because the router filter runs after ext_proc.

9. **iptables backend auto-detection**: `init-iptables.sh` auto-detects `iptables-legacy` vs `iptables-nft`. Override with `IPTABLES_CMD` env var if needed. Always verify with proxy-init logs after deployment.
