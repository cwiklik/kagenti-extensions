# toolhive-webhook

A Kubernetes admission webhook for [ToolHive](https://github.com/stacklok/toolhive) MCPServer resources that automatically injects sidecar containers to enable Keycloak client registration and SPIFFE/SPIRE token exchanges for secure service-to-service authentication within the Kagenti platform.

## Overview

This webhook provides zero-configuration security for MCPServer resources by automatically injecting two sidecar containers that handle identity and authentication:

1. **`spiffe-helper`** - Obtains SPIFFE Verifiable Identity Documents (SVIDs) from the SPIRE agent via the Workload API
2. **`kagenti-client-registration`** - Registers the MCPServer as an OAuth2 client in Keycloak using the SPIFFE identity

When an MCPServer resource is created or updated, the webhook automatically:

- Injects both sidecar containers into the pod specification
- Mounts necessary volumes for SPIRE agent communication and credential sharing
- Configures environment variables for Keycloak integration
- Ensures proper container orchestration (SPIFFE helper runs first, then client registration waits for tokens)

## Architecture

```

┌─────────────────────────────────────────────────────────┐
│                    MCPServer Pod                        │
│                                                         │
│  ┌─────────────────┐  ┌──────────────────────────────┐  │
│  │ spiffe-helper   │  │ kagenti-client-registration  │  │
│  │                 │  │                              │  │
│  │ 1. Connects to  │  │ 2. Waits for jwt_svid.token  │  │
│  │    SPIRE agent  │──│    in /opt/                  │  │
│  │ 2. Gets JWT-SVID│  │ 3. Registers with Keycloak   │  │
│  │ 3. Writes to    │  │    using SPIFFE identity     │  │
│  │    /opt/jwt_    │  │ 4. Runs continuously         │  │
│  │    svid.token   │  │                              │  │
│  └─────────────────┘  └──────────────────────────────┘  │
│           │                        │                    │
│  ┌────────▼────────────────────────▼───────────────────┐│
│  │        Shared Volume: svid-output (/opt)            ││
│  └─────────────────────────────────────────────────────┘│
│                                                         │
│  ┌────────────────────────────────────┐                 │
│  │    Your MCPServer Container        │                 │
│  │  (authenticated via Keycloak)      │                 │
│  └────────────────────────────────────┘                 │
└─────────────────────────────────────────────────────────┘
         │                           │
         ▼                           ▼
  SPIRE Agent Socket          Keycloak Server
  (/run/spire/agent-sockets)  (OAuth2/OIDC)
```

## Features

### Automatic Sidecar Injection

The webhook injects two sidecar containers:

#### 1. SPIFFE Helper (`spiffe-helper`)

- **Image**: `ghcr.io/spiffe/spiffe-helper:nightly`
- **Purpose**: Obtains and refreshes JWT-SVIDs from SPIRE
- **Resources**: 50m CPU / 64Mi memory (request), 100m CPU / 128Mi memory (limit)
- **Volumes**:
  - `/spiffe-workload-api` - SPIRE agent socket
  - `/etc/spiffe-helper` - Configuration
  - `/opt` - SVID token output

#### 2. Client Registration (`kagenti-client-registration`)

- **Image**: `ghcr.io/kagenti/kagenti/client-registration:latest`
- **Purpose**: Registers MCPServer as Keycloak OAuth2 client using SPIFFE identity
- **Resources**: 50m CPU / 64Mi memory (request), 100m CPU / 128Mi memory (limit)
- **Behavior**: Waits for `/opt/jwt_svid.token`, then registers with Keycloak
- **Volumes**:
  - `/opt` - Reads SVID token from spiffe-helper

### Automatic Volume Configuration

The webhook automatically adds these volumes:

- **`shared-data`** - EmptyDir for inter-container communication
- **`spire-agent-socket`** - HostPath to `/run/spire/agent-sockets` for SPIRE agent access
- **`spiffe-helper-config`** - ConfigMap containing SPIFFE helper configuration
- **`svid-output`** - EmptyDir for SVID token exchange between sidecars

### Configuration

The webhook supports the following flags:

- `--enable-client-registration` - Enable automatic sidecar injection (default: true)
- `--webhook-cert-path` - Directory containing webhook TLS certificates (default: /tmp/k8s-webhook-server/serving-certs)
- `--webhook-cert-name` - Webhook certificate filename (default: tls.crt)
- `--webhook-cert-key` - Webhook key filename (default: tls.key)



3. **SPIRE Agent** - Must be running with socket at `/run/spire/agent-sockets/spire-agent.sock`

## Getting Started

### Prerequisites

- Kubernetes v1.11.3+ cluster
- Go v1.22+ (for development)
- Docker v17.03+ (for building images)
- kubectl v1.11.3+
- cert-manager v1.0+ (for webhook TLS certificates)
- SPIRE agent deployed on cluster nodes
- Keycloak server accessible from the cluster

### Quick Start with Helm

```bash

# Install the webhook using Helm
helm install toolhive-webhook oci://ghcr.io/kagenti/toolhive-webhook/toolhive-webhook-chart \
  --version <version> \
  --namespace kagenti-system \
  --create-namespace
```

### Local Development with Kind

```bash
cd toolhive-webhook

# Build and deploy to local Kind cluster in one command
make local-dev CLUSTER=<your-kind-cluster-name>

# Or step by step:
make ko-local-build                    # Build with ko
make kind-load-image CLUSTER=<name>    # Load into Kind
make install-local-chart CLUSTER=<name> # Deploy with Helm

# Reinstall after changes
make reinstall-local-chart CLUSTER=<name>
```

### Traditional Deployment

**Build and push your image:**

```sh
make docker-build docker-push IMG=<registry>/toolhive-webhook:tag
```

**Deploy using kustomize:**

```sh
make deploy IMG=<registry>/toolhive-webhook:tag
```

**Or deploy using Helm:**

```sh
helm install toolhive-webhook ./charts/toolhive-webhook \
  --set image.repository=<registry>/toolhive-webhook \
  --set image.tag=<tag> \
  --namespace kagenti-system \
  --create-namespace
```

## How It Works

### Mutation Process

1. **MCPServer Created** - User creates a MCPServer custom resource
2. **Webhook Intercepts** - Kubernetes API server sends the resource to the webhook
3. **Containers Injected** - Webhook adds `spiffe-helper` and `kagenti-client-registration` sidecars
4. **Volumes Added** - Required volumes for SPIRE, configuration, and credential sharing
5. **Resource Applied** - Modified MCPServer is created in the cluster

### Runtime Behavior

1. **Pod Starts** - All containers start simultaneously
2. **SPIFFE Helper** - Connects to SPIRE agent, obtains JWT-SVID, writes to `/opt/jwt_svid.token`
3. **Client Registration** - Waits for token file, then registers with Keycloak using SPIFFE identity
4. **MCPServer Container** - Starts and can authenticate via Keycloak using registered client credentials

### Security Model

- **SPIFFE/SPIRE** - Provides cryptographic workload identity
- **JWT-SVID** - Short-lived, automatically rotated tokens
- **Keycloak** - Central authentication and authorization
- **Zero Trust** - Every service authenticated via SPIFFE identity
- **No Static Credentials** - All tokens dynamically generated and rotated

## Configuration

### Webhook Configuration

The webhook can be configured via Helm values or command-line flags:

```yaml
# values.yaml
webhook:
  enabled: true
  enableClientRegistration: true  # Enable sidecar injection
  certPath: /tmp/k8s-webhook-server/serving-certs
  certName: tls.crt
  certKey: tls.key
  port: 9443
```

### Disable Sidecar Injection

To disable sidecar injection for specific MCPServers, the webhook can be configured globally:

```yaml
webhook:
  enableClientRegistration: false
```

Or deploy with the flag:
```bash
make deploy IMG=<image> ENABLE_CLIENT_REGISTRATION=false
```



## License

Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
