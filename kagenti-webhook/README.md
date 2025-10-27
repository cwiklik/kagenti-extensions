# kagenti-webhook

A Kubernetes admission webhook for [ToolHive](https://github.com/stacklok/toolhive) MCPServer and Kagenti Agent resources that automatically injects sidecar containers to enable Keycloak client registration and SPIFFE/SPIRE token exchanges for secure service-to-service authentication within the Kagenti platform.

## Overview

This webhook provides security for both MCPServer and Agent resources by automatically injecting two sidecar containers that handle identity and authentication:

1. **`spiffe-helper`** - Obtains SPIFFE Verifiable Identity Documents (SVIDs) from the SPIRE agent via the Workload API
2. **`kagenti-client-registration`** - Registers the resource as an OAuth2 client in Keycloak using the SPIFFE identity

### Why Sidecar Injection?

The sidecar approach is necessary because the ToolHive proxy is not currently designed to be easily extensible. Implementing Kagenti's authentication and authorization requirements would require modifications to the ToolHive proxy codebase to add middleware plugin support.

For both Agent CR and MCPServer CR resources, sidecar containers provide a consistent pattern for extending functionality without modifying upstream components.

## Supported Resources

The webhook supports sidecar injection for:

1. **MCPServer** (group: `toolhive.stacklok.dev/v1alpha1`) - ToolHive MCP servers
2. **Agent** (group: `agent.kagenti.dev/v1alpha1`) - Kagenti agents (from [kagenti-operator](https://github.com/kagenti/kagenti-operator))

Both resource types receive identical sidecar container injection using a **common pod mutation code**.

## Istio-Style Namespace Injection

The webhook supports flexible injection control via namespace labels and annotations, similar to Istio's sidecar injection pattern.

### Enable Injection for Entire Namespace

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: my-apps
  labels:
    kagenti-enabled: "true"  # All Agent/MCPServer CRs in this namespace get sidecars
```

Now all Agents and MCPServers created in the `my-apps` namespace automatically get sidecars:

```yaml
# Both of these get sidecars automatically!
apiVersion: agent.kagenti.dev/v1alpha1
kind: Agent
metadata:
  name: my-agent
  namespace: my-apps
spec:
  podTemplateSpec:
    spec:
      containers:
      - name: agent
        image: my-agent:latest
---
apiVersion: toolhive.stacklok.dev/v1alpha1
kind: MCPServer
metadata:
  name: my-server
  namespace: my-apps
spec:
  podTemplateSpec:
    spec:
      containers:
      - name: server
        image: my-server:latest
```

### Per-Resource Control

**Opt-out (skip injection even if namespace has it enabled):**
```yaml
apiVersion: agent.kagenti.dev/v1alpha1
kind: Agent
metadata:
  name: no-injection-agent
  namespace: my-apps  # Has kagenti-injection=enabled
  annotations:
    kagenti.dev/inject: "false"  # Explicit opt-out
```

**Opt-in (force injection even if namespace doesn't have label):**
```yaml
apiVersion: toolhive.stacklok.dev/v1alpha1
kind: MCPServer
metadata:
  name: force-injection-server
  namespace: other-namespace  # No label
  annotations:
    kagenti.dev/inject: "true"  # Explicit opt-in
```

### Injection Priority (Highest to Lowest)

1. **CR Annotation (opt-out)**: `kagenti.dev/inject: "false"` - Explicit disable
2. **CR Annotation (opt-in)**: `kagenti.dev/inject: "true"` - Explicit enable
3. **Namespace Label**: `kagenti-enabled: true` - Namespace-wide enable
4. **Namespace Annotation**: `kagenti.dev/inject: "true"` - Namespace-wide enable

## Architecture

```
┌────────────────────────────────────────────────────────-─┐
│              MCPServer/Agent Pod                         │
│                                                          │
│  ┌─────────────────┐  ┌──────────────────────────────┐   │
│  │ spiffe-helper   │  │ kagenti-client-registration  │   │
│  │                 │  │                              │   │
│  │ 1. Connects to  │  │ 2. Waits for jwt_svid.token  │   │
│  │    SPIRE agent  │──│    in /opt/                  │   │
│  │ 2. Gets JWT-SVID│  │ 3. Registers with Keycloak   │   │
│  │ 3. Writes to    │  │    using SPIFFE identity     │   │
│  │    /opt/jwt_    │  │ 4. Runs continuously         │   │
│  │    svid.token   │  │                              │   │
│  └─────────────────┘  └──────────────────────────────┘   │
│           │                        │                     │
│  ┌────────▼────────────────────────▼───────────────────┐ │
│  │        Shared Volume: svid-output (/opt)            │ │
│  └─────────────────────────────────────────────────────┘ │
│                                                          │
│  ┌────────────────────────────────────┐                  │
│  │    Your Application Container      │                  │
│  │    (Agent or MCPServer)            │                  │
│  │  (authenticated via Keycloak)      │                  │
│  └────────────────────────────────────┘                  │
└──────────────────────────────────────────────────-───────┘
         │                           │
         ▼                           ▼
  SPIRE Agent Socket          Keycloak Server
  (/run/spire/agent-sockets)  (OAuth2/OIDC)
```

For detailed architecture diagrams, see [`ARCHITECTURE.md`](../ARCHITECTURE.md).

## Features

### Automatic Sidecar Injection

The webhook injects two sidecar containers into every Agent and MCPServer:

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
- **Purpose**: Registers resource as Keycloak OAuth2 client using SPIFFE identity
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
helm install kagenti-webhook oci://ghcr.io/kagenti/kagenti-extensions/kagenti-webhook-chart \
  --version <version> \
  --namespace kagenti-webhook-system \
  --create-namespace
```

### Local Development with Kind

```bash
cd kagenti-webhook

# Build and deploy to local Kind cluster in one command
make local-dev CLUSTER=<your-kind-cluster-name>

# Or step by step:
make ko-local-build                    # Build with ko
make kind-load-image CLUSTER=<name>    # Load into Kind
make install-local-chart CLUSTER=<name> # Deploy with Helm

# Reinstall after changes
make reinstall-local-chart CLUSTER=<name>
```

### Webhook Configuration

The webhook can be configured via Helm values or command-line flags:

```yaml
# values.yaml
webhook:
  enabled: true
  certPath: /tmp/k8s-webhook-server/serving-certs
  certName: tls.crt
  certKey: tls.key
  port: 9443
```


## Development

### Shared Pod-Mutator Architecture

The webhook uses a shared pod mutation engine to eliminate code duplication:

```bash
internal/webhook/
├── injector/                    # Shared mutation logic
│   ├── pod_mutator.go          # Core mutation engine
│   ├── namespace_checker.go    # Namespace inspection
│   ├── container_builder.go    # Build sidecars
│   └── volume_builder.go       # Build volumes
└── v1alpha1/
    ├── mcpserver_webhook.go    # MCPServer webhook
    └── agent_webhook.go         # Agent webhook
```

Both webhooks use the same `PodMutator` instance, ensuring:

- Consistent behavior across resource types
- Single source of truth for injection logic
- Easy to add new resource types or features


## Uninstallation

### Using Helm

```bash
helm uninstall kagenti-webhook -n kagenti-webhook-system
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
