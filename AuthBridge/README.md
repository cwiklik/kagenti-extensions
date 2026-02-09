# AuthBridge

AuthBridge provides **secure, transparent token management** for Kubernetes workloads. It combines automatic [client registration](./client-registration/) with [token exchange](./AuthProxy/) capabilities, enabling zero-trust authentication flows with [SPIFFE/SPIRE](https://spiffe.io) integration.

> **📘 Looking to run the demo?** See the [Demo Guide](./demo.md) for step-by-step instructions.

## What AuthBridge Does

AuthBridge solves the challenge of **secure service-to-service authentication** in Kubernetes:

1. **Automatic Identity** - Workloads automatically obtain their identity from SPIFFE/SPIRE and register as Keycloak clients using their SPIFFE ID (e.g., `spiffe://example.com/ns/default/sa/myapp`)

2. **Token-Based Authorization** - Callers obtain JWT tokens from Keycloak with the workload's identity as the audience, authorizing them to invoke specific services

3. **Transparent Token Exchange** - A sidecar intercepts outgoing requests, validates incoming tokens, and exchanges them for tokens with the appropriate target audience—all without application code changes

4. **Target Service Validation** - Target services validate the exchanged token, ensuring it has the correct audience before authorizing requests

## Architecture

```
┌────────────────────────────────────────────────────────────────────────┐
│                          WORKLOAD POD                                  │
│                    (with AuthBridge sidecars)                          │
│                                                                        │
│  ┌─────────────────────────────────────────────────────────────────┐   │
│  │  Init Container: proxy-init (iptables setup)                    │   │
│  └─────────────────────────────────────────────────────────────────┘   │
│                                                                        │
│  ┌─────────────────────────────────────────────────────────────────┐   │
│  │                      Containers                                 │   │
│  │  ┌──────────────┐  ┌─────────────────┐  ┌────────────────────┐  │   │
│  │  │  Your App    │  │  SPIFFE Helper  │  │      AuthProxy     │  │   │
│  │  │              │  │  (provides      │  │  (Envoy + Ext Proc │  │   │
│  │  │              │  │   SPIFFE creds) │  │  = token exchange) │  │   │
│  │  └──────┬───────┘  └─────────────────┘  └──────────┬─────────┘  │   │
│  │                                                                 │   │
│  │  ┌───────────────────────────────────────────────────────────┐  │   │
│  │  │ client-registration (registers Workload with Keycloak)    │  │   │
│  │  └───────────────────────────────────────────────────────────┘  │   │
│  └─────────┼───────────────────────────────────────────┼───────────┘   │
│            │ Caller's token (aud: SPIFFE ID)           │               │
│            └───────────────────────────────────────────┘               │
│                              │                                         │
└──────────────────────────────┼─────────────────────────────────────────┘
                               │ Token exchanged for target-service audience
                               │ (using Workload's own credentials)
                               ▼
                    ┌─────────────────────┐
                    │  TARGET SERVICE POD │
                    │                     │
                    │  Validates token    │
                    │  with audience      │
                    │  "target-service"   │
                    └─────────────────────┘
```

<details>
<summary><b>📊 Mermaid Architecture Diagram (click to expand)</b></summary>

```mermaid
flowchart TB
    subgraph WorkloadPod["WORKLOAD POD (with AuthBridge sidecars)"]
        subgraph Init["Init Container"]
            ProxyInit["proxy-init<br/>(iptables setup)"]
        end
        subgraph Containers["Containers"]
            App["Your Application"]
            SpiffeHelper["SPIFFE Helper<br/>(provides SVID)"]
            ClientReg["client-registration<br/>(registers with Keycloak)"]
            subgraph Sidecar["AuthProxy Sidecar"]
                AuthProxy["auth-proxy"]
                Envoy["envoy-proxy"]
                ExtProc["ext-proc"]
            end
        end
    end

    subgraph TargetPod["TARGET SERVICE POD"]
        Target["Target Service<br/>(validates tokens)"]
    end

    subgraph External["External Services"]
        SPIRE["SPIRE Agent"]
        Keycloak["Keycloak"]
    end

    Caller["Caller<br/>(external)"]

    SPIRE --> SpiffeHelper
    SpiffeHelper --> ClientReg
    ClientReg --> Keycloak
    Caller -->|"1. Get token"| Keycloak
    Caller -->|"2. Pass token"| App
    App -->|"3. Request + Token"| Envoy
    Envoy --> ExtProc
    ExtProc -->|"4. Token Exchange"| Keycloak
    Envoy -->|"5. Request + Exchanged Token"| Target
    Target -->|"6. Response"| App
    App -->|"7. Response"| Caller

    style WorkloadPod fill:#e1f5fe
    style TargetPod fill:#e8f5e9
    style Sidecar fill:#fff3e0
    style External fill:#fce4ec
    style Caller fill:#fff9c4
```

</details>

## Components

### Workload Pod (AuthBridge Sidecars)

| Container | Type | Purpose |
|-----------|------|---------|
| `proxy-init` | init | Sets up iptables to intercept inbound and outbound traffic (excludes Keycloak port) |
| `client-registration` | container | Registers workload with Keycloak using SPIFFE ID, saves credentials to `/shared/` |
| `spiffe-helper` | container | Provides SPIFFE credentials (SVID) |
| `auth-proxy` | container | Pass-through proxy (JWT validation handled by Ext Proc on inbound path) |
| `envoy-proxy` | container | Intercepts inbound traffic (JWT validation) and outbound traffic (HTTP: token exchange via Ext Proc; HTTPS: TLS passthrough) |

### Target Service Pod

Any downstream service that validates incoming tokens have the expected audience.

## End-to-End Flow

**Initialization (Workload Pod Startup):**
```
  SPIRE Agent             Workload Pod                        Keycloak
       │                        │                                │
       │  0. SVID               │                                │
       │───────────────────────►│  SPIFFE Helper                 │
       │  (SPIFFE ID)           │                                │
       │                        │                                │
       │                        │  1. Register client            │
       │                        │  (client_id = SPIFFE ID)       │
       │                        │───────────────────────────────►│
       │                        │  Client Registration           │
       │                        │                                │
       │                        │◄───────────────────────────────│
       │                        │  client_secret                 │
       │                        │  (saved to /shared/)           │
```

**Runtime Flow:**
```
  Caller             Workload Pod              Keycloak      Target Service
    │                     │                        │               │
    │  2. Get token       │                        │               │
    │  (aud: Workload's SPIFFE ID)                 │               │
    │─────────────────────────────────────────────►│               │
    │◄─────────────────────────────────────────────│               │
    │  Token (aud: Workload)                       │               │
    │                     │                        │               │
    │  3. Pass token      │                        │               │
    │  to Workload        │                        │               │
    │────────────────────►│                        │               │
    │                     │                        │               │
    │                     │  4. Workload calls     │               │
    │                     │  Target Service with   │               │
    │                     │  Caller's token        │               │
    │                     │──────────┐             │               │
    │                     │          │             │               │
    │                     │  AuthProxy intercepts  │               │
    │                     │  validates aud         │               │
    │                     │          │             │               │
    │                     │  5. Token Exchange     │               │
    │                     │  (using Workload creds)│               │
    │                     │───────────────────────►│               │
    │                     │◄───────────────────────│               │
    │                     │  New token (aud: target-service)       │
    │                     │          │             │               │
    │                     │  6. Forward request    │               │
    │                     │  with exchanged token  │               │
    │                     │───────────────────────────────────────►│
    │                     │                        │               │
    │                     │◄───────────────────────────────────────│
    │                     │  "authorized"          │               │
    │◄────────────────────│                        │               │
    │  Response           │                        │               │
```

## What Gets Verified

| Step | Component | Verification |
|------|-----------|--------------|
| 0 | SPIFFE Helper | SVID obtained from SPIRE Agent |
| 1 | Client Registration | Workload registered with Keycloak (client_id = SPIFFE ID) |
| 2 | Caller | Token obtained with `aud: Workload's SPIFFE ID` |
| 3 | Workload | Token received from Caller |
| 4 | AuthProxy | Token validated (aud matches Workload's identity) |
| 5 | Ext Proc | Token exchanged using Workload's credentials → `aud: target-service` |
| 6 | Target Service | Token validated, returns `"authorized"` |

## Detailed End-to-End Flow

<details>
<summary><b>📊 Mermaid Diagram (click to expand)</b></summary>

```mermaid
sequenceDiagram
    autonumber
    participant SPIRE as SPIRE Agent
    participant Helper as SPIFFE Helper
    participant Reg as Client Registration
    participant Caller as Caller
    participant App as Workload
    participant Envoy as AuthProxy (Envoy + Ext Proc)
    participant KC as Keycloak
    participant Target as Target Service

    Note over Helper,SPIRE: Workload Pod Initialization
    SPIRE->>Helper: SVID (SPIFFE credentials)
    Helper->>Reg: JWT with SPIFFE ID
    Reg->>KC: Register client (client_id = SPIFFE ID)
    KC-->>Reg: Client credentials (saved to /shared/)

    Note over Caller,Target: Runtime Flow
    Caller->>KC: Get token (aud: Workload's SPIFFE ID)
    KC-->>Caller: Token with workload-aud scope
    
    Caller->>App: Pass token
    App->>Envoy: Call Target Service with Caller's token
    
    Note over Envoy: AuthProxy intercepts<br/>Validates aud = Workload's ID<br/>Uses Workload's credentials
    
    Envoy->>KC: Token Exchange (Workload's creds)
    KC-->>Envoy: New Token (aud: target-service)
    
    Envoy->>Target: Request + Exchanged Token
    Target->>Target: Validate token (aud: target-service)
    Target-->>App: "authorized"
    App-->>Caller: Response
```

### Detailed Flow Summary

| Step | From → To | Action |
|------|-----------|--------|
| **Initialization Phase** |||
| 1 | SPIRE → SPIFFE Helper | Issue SVID (SPIFFE credentials) |
| 2 | SPIFFE Helper → Client Registration | Pass JWT with SPIFFE ID |
| 3 | Client Registration → Keycloak | Register client (`client_id` = SPIFFE ID) |
| 4 | Keycloak → Client Registration | Return client credentials (saved to `/shared/`) |
| **Runtime Phase** |||
| 5 | Caller → Keycloak | Request token (`aud`: Workload's SPIFFE ID) |
| 6 | Keycloak → Caller | Return token with workload-aud scope |
| 7 | Caller → Workload | Pass token to Workload |
| 8 | Workload → AuthProxy | Call Target Service with Caller's token |
| 9 | AuthProxy → Keycloak | Token Exchange (using Workload's credentials) |
| 10 | Keycloak → AuthProxy | Return new token (`aud`: target-service) |
| 11 | AuthProxy → Target Service | Forward request with exchanged token |
| 12 | Target Service | Validate token (`aud`: target-service) |
| 13 | Target Service → Workload | Return "authorized" |
| 14 | Workload → Caller | Return response |

</details>

## Key Security Properties

- **No Static Secrets** - Credentials are dynamically generated during registration
- **Short-Lived Tokens** - JWT tokens expire and must be refreshed
- **Self-Audience Scoping** - Tokens include the Workload's own identity as audience, enabling token exchange
- **Same Identity for Exchange** - AuthProxy uses the Workload's credentials (same SPIFFE ID), matching the token's audience
- **Transparent to Application** - Token exchange is handled by the sidecar; applications don't need to implement it
- **Configurable Target** - Target audience and scopes are configured via Kubernetes Secret

## Prerequisites

- Kubernetes cluster (Kind recommended for local development)
- SPIRE installed and running (server + agent) - for SPIFFE version
- Keycloak deployed
- Docker/Podman for building images

### Quick Setup

The easiest way to get all prerequisites is to use the [Kagenti Ansible installer](https://github.com/kagenti/kagenti/blob/main/docs/install.md#ansible-based-installer-recommended).

## Getting Started

See the **[Demo Guide](./demo.md)** for complete step-by-step instructions on:

- Building and loading images
- Configuring Keycloak
- Deploying the demo
- Testing the token exchange flow
- Inspecting token claims
- Troubleshooting common issues

## Component Documentation

- [AuthProxy](AuthProxy/README.md) - Token validation and exchange proxy
- [Client Registration](client-registration/README.md) - Automatic Keycloak client registration with SPIFFE

## References

- [Kagenti Installation](https://github.com/kagenti/kagenti/blob/main/docs/install.md)
- [SPIRE Documentation](https://spiffe.io/docs/latest/)
- [OAuth 2.0 Token Exchange (RFC 8693)](https://www.rfc-editor.org/rfc/rfc8693)
