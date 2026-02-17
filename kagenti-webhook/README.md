# kagenti-webhook

A Kubernetes admission webhook that automatically injects sidecar containers to enable Keycloak client registration and optional SPIFFE/SPIRE token exchanges for secure service-to-service authentication within the Kagenti platform.

## ⚠️ Deprecation Notice

**The Agent CR and MCPServer CR webhooks are deprecated** and will be removed in a future release.

**Please migrate to the AuthBridge webhook** which supports standard Kubernetes workloads (Deployments, StatefulSets, DaemonSets, Jobs, CronJobs). See the [Migration Guide](#migration-guide) below for details.

## Overview

This webhook provides security by automatically injecting sidecar containers that handle identity and authentication. It supports both standard Kubernetes workloads (Deployments, StatefulSets, etc.) and custom resources.

The webhook injects:

1. **`proxy-init`** (init container) - Configures iptables rules for traffic interception
2. **`envoy-proxy`** - Service mesh proxy for traffic management
3. **`spiffe-helper`** (optional, if SPIRE enabled) - Obtains SPIFFE Verifiable Identity Documents (SVIDs) from the SPIRE agent via the Workload API
4. **`kagenti-client-registration`** (optional, if SPIRE enabled) - Registers the resource as an OAuth2 client in Keycloak using the SPIFFE identity

### Why Sidecar Injection?

The sidecar approach provides a consistent pattern for extending functionality without modifying application code or upstream components.

## Supported Resources

The webhook supports sidecar injection for:

### AuthBridge Webhook (Recommended)

The **AuthBridge webhook** is the primary, actively maintained webhook that supports standard Kubernetes workload resources:

- **Deployments** (apps/v1)
- **StatefulSets** (apps/v1)
- **DaemonSets** (apps/v1)
- **Jobs** (batch/v1)
- **CronJobs** (batch/v1)

All workloads use the **common pod mutation code** for consistent behavior.

### Legacy Webhooks (Deprecated)

> **⚠️ DEPRECATION NOTICE**: The following webhooks are deprecated and will be removed in a future release. Please migrate to the AuthBridge webhook with standard Kubernetes workloads.

1. **MCPServer** (group: `toolhive.stacklok.dev/v1alpha1`) - ToolHive MCP servers *(deprecated)*
2. **Agent** (group: `agent.kagenti.dev/v1alpha1`) - Kagenti agents from [kagenti-operator](https://github.com/kagenti/kagenti-operator) *(deprecated)*

## Injection Control

The webhook supports flexible injection control via a multi-layer precedence system. Each sidecar's injection decision is evaluated independently through a chain of layers, where the first "no" short-circuits.

### Precedence Chain

For the AuthBridge webhook, each sidecar (`envoy-proxy`, `spiffe-helper`, `client-registration`) is evaluated through this chain:

```text
Global Feature Gate → Per-Sidecar Feature Gate → Namespace Label → Workload Label → Platform Defaults → Inject
```

| Layer | Scope | How to configure | Effect |
| --- | --- | --- | --- |
| Global Feature Gate | Cluster-wide | `featureGates.globalEnabled` in Helm values | Kill switch — disables ALL sidecar injection |
| Per-Sidecar Feature Gate | Cluster-wide, per sidecar | `featureGates.envoyProxy`, `.spiffeHelper`, `.clientRegistration` in Helm values | Disables a specific sidecar cluster-wide |
| Namespace Label | Namespace | `kagenti-enabled: "true"` label on namespace | Opts a namespace into injection |
| Workload Label | Per-workload, per sidecar | `kagenti.io/envoy-proxy-inject: "false"` (etc.) on pod template | Disables a specific sidecar for one workload |
| Platform Defaults | Cluster-wide, per sidecar | `defaults.sidecars.<sidecar>.enabled` in Helm values | Lowest-priority default |

The `proxy-init` init container always follows the `envoy-proxy` decision (it is required for envoy to function).

### Feature Gates

Feature gates provide cluster-wide control over sidecar injection. They are configured via Helm values and deployed as a ConfigMap with hot-reload support.

```yaml
# values.yaml
featureGates:
  globalEnabled: true        # Kill switch — set to false to disable ALL injection
  envoyProxy: true           # Set to false to disable envoy-proxy cluster-wide
  spiffeHelper: true         # Set to false to disable spiffe-helper cluster-wide
  clientRegistration: true   # Set to false to disable client-registration cluster-wide
```

### Namespace-Level Injection

Enable injection for all eligible workloads in a namespace:

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: my-apps
  labels:
    kagenti-enabled: "true"  # All eligible workloads in this namespace get sidecars
```

### Workload-Level Control

Workloads must have the `kagenti.io/type` label set to `agent` or `tool` to be eligible for injection. Without this label, injection is always skipped.

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-app
  namespace: my-apps       # Namespace must have kagenti-enabled=true
spec:
  template:
    metadata:
      labels:
        app: my-app
        kagenti.io/type: agent        # Required: Identifies workload type (agent or tool)
        kagenti.io/spire: enabled     # Optional: Enable SPIFFE/SPIRE integration
    spec:
      containers:
      - name: app
        image: my-app:latest
```

### Per-Sidecar Workload Labels

Individual sidecars can be disabled per-workload using these labels on the pod template:

| Label | Controls |
| --- | --- |
| `kagenti.io/envoy-proxy-inject: "false"` | Disables envoy-proxy (and proxy-init) |
| `kagenti.io/spiffe-helper-inject: "false"` | Disables spiffe-helper |
| `kagenti.io/client-registration-inject: "false"` | Disables client-registration |

Example — disable envoy-proxy for a specific workload:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: no-envoy-app
  namespace: my-apps
spec:
  template:
    metadata:
      labels:
        kagenti.io/type: agent
        kagenti.io/envoy-proxy-inject: "false"   # Skip envoy for this workload
    spec:
      containers:
      - name: app
        image: my-app:latest
```

### SPIRE Integration Control

The AuthBridge webhook supports optional SPIRE integration. Use the `kagenti.io/spire` label on pod templates:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-app-with-spire
  namespace: my-apps
spec:
  template:
    metadata:
      labels:
        kagenti.io/type: agent
        kagenti.io/spire: enabled  # Enables spiffe-helper and client-registration sidecars
    spec:
      containers:
      - name: app
        image: my-app:latest
```

Without the `kagenti.io/spire: enabled` label, the spiffe-helper container is not injected even if its precedence decision is "inject".

### Platform Configuration

Container images, resource limits, proxy settings, and other parameters are externalized into a ConfigMap managed through Helm values. The webhook loads this configuration at startup and supports hot-reload via file watching.

```yaml
# values.yaml — defaults section
defaults:
  images:
    envoyProxy: ghcr.io/kagenti/kagenti-extensions/envoy-with-processor:latest
    proxyInit: ghcr.io/kagenti/kagenti-extensions/proxy-init:latest
    spiffeHelper: ghcr.io/spiffe/spiffe-helper:nightly
    clientRegistration: ghcr.io/kagenti/kagenti-extensions/client-registration:latest
    pullPolicy: IfNotPresent
  proxy:
    port: 15123
    uid: 1337
    adminPort: 9901
    inboundProxyPort: 15124
  resources:
    envoyProxy:
      requests: { cpu: 200m, memory: 256Mi }
      limits: { cpu: 50m, memory: 64Mi }
    # ... (proxyInit, spiffeHelper, clientRegistration)
  sidecars:
    envoyProxy: { enabled: true }
    spiffeHelper: { enabled: true }
    clientRegistration: { enabled: true }
```

If the ConfigMap is not available, compiled defaults are used as a fallback.

### Legacy Webhook Injection Priority (Deprecated)

**For legacy webhooks (CR annotations):**
1. **CR Annotation (opt-out)**: `kagenti.dev/inject: "false"` - Explicit disable
2. **CR Annotation (opt-in)**: `kagenti.dev/inject: "true"` - Explicit enable
3. **Namespace Label**: `kagenti-enabled: "true"` - Namespace-wide enable
4. **Namespace Annotation**: `kagenti.dev/inject: "true"` - Namespace-wide enable

## Architecture

### AuthBridge Architecture

The AuthBridge webhook supports two modes of operation:

#### With SPIRE Integration (`kagenti.io/spire: enabled` label)

```
┌────────────────────────────────────────────────────────────────┐
│                    Kubernetes Workload Pod                     │
│                                                                │
│  ┌──────────────┐  ┌────────────┐  ┌─────────────────────────┐│
│  │  proxy-init  │  │ envoy-proxy│  │   spiffe-helper         ││
│  │ (init)       │  │            │  │                         ││
│  │ - Setup      │  │ - Service  │  │ 1. Connects to SPIRE    ││
│  │   iptables   │  │   mesh     │  │ 2. Gets JWT-SVID        ││
│  │ - Redirect   │  │   proxy    │  │ 3. Writes jwt_svid.token││
│  │   traffic    │  │            │  │                         ││
│  └──────────────┘  └────────────┘  └─────────────────────────┘│
│                                              │                 │
│                          ┌───────────────────▼──────────────┐  │
│  ┌─────────────────────┐ │    Shared Volume: /opt          │  │
│  │ client-registration │─│                                  │  │
│  │                     │ └──────────────────────────────────┘  │
│  │ 1. Waits for token  │                                       │
│  │ 2. Registers with   │                                       │
│  │    Keycloak         │                                       │
│  └─────────────────────┘                                       │
│                                                                │
│  ┌──────────────────────────────────────────────────────────┐ │
│  │              Your Application Container                   │ │
│  │          (Deployment/StatefulSet/Job/etc.)               │ │
│  └──────────────────────────────────────────────────────────┘ │
└────────────────────────────────────────────────────────────────┘
         │                    │                    │
         ▼                    ▼                    ▼
  SPIRE Agent Socket   Keycloak Server      Application Traffic
                       (OAuth2/OIDC)        (via Envoy proxy)
```

#### Without SPIRE Integration (default, no `kagenti.io/spire` label)

```
┌────────────────────────────────────────────────────────────────┐
│                    Kubernetes Workload Pod                     │
│                                                                │
│  ┌──────────────┐  ┌────────────────────────────────────────┐ │
│  │  proxy-init  │  │           envoy-proxy                  │ │
│  │ (init)       │  │                                        │ │
│  │ - Setup      │  │ - Service mesh proxy                   │ │
│  │   iptables   │  │ - Traffic management                   │ │
│  │ - Redirect   │  │ - Authentication (non-SPIFFE methods)  │ │
│  │   traffic    │  │                                        │ │
│  └──────────────┘  └────────────────────────────────────────┘ │
│                                                                │
│  ┌──────────────────────────────────────────────────────────┐ │
│  │              Your Application Container                   │ │
│  │          (Deployment/StatefulSet/Job/etc.)               │ │
│  └──────────────────────────────────────────────────────────┘ │
└────────────────────────────────────────────────────────────────┘
                               │
                               ▼
                       Application Traffic
                        (via Envoy proxy)
```

### Legacy Webhook Architecture

The legacy Agent CR and MCPServer CR webhooks inject only SPIRE-based sidecars:

```
┌────────────────────────────────────────────────────────-─┐
│              MCPServer/Agent Pod (DEPRECATED)            │
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
│  │    (Agent or MCPServer CR)         │                  │
│  │  (authenticated via Keycloak)      │                  │
│  └────────────────────────────────────┘                  │
└──────────────────────────────────────────────────-───────┘
         │                           │
         ▼                           ▼
  SPIRE Agent Socket          Keycloak Server
  (/run/spire/agent-sockets)  (OAuth2/OIDC)
```

For detailed architecture diagrams, see [`ARCHITECTURE.md`](ARCHITECTURE.md).

## Features

### Automatic Container Injection

#### AuthBridge Containers

The AuthBridge webhook injects the following containers into Kubernetes workloads:

**Always injected:**

##### 1. Proxy Init (`proxy-init`) - Init Container

- **Image**: `ghcr.io/kagenti/kagenti-extensions/proxy-init:latest` (configurable)
- **Purpose**: Sets up iptables rules to redirect traffic through the Envoy proxy
- **Resources**: 10m CPU / 64Mi memory (request/limit)
- **Privileged**: Yes (required for iptables modification)

##### 2. Envoy Proxy (`envoy-proxy`) - Sidecar Container

- **Image**: `ghcr.io/kagenti/kagenti-extensions/envoy-with-processor:latest` (configurable)
- **Purpose**: Service mesh proxy for traffic management and authentication
- **Resources**: 50m CPU / 64Mi memory (request), 200m CPU / 256Mi memory (limit)
- **Ports**: 15123 (envoy-outbound), 9901 (admin), 9090 (ext-proc)

**Injected when `kagenti.io/spire: enabled` label is set:**

##### 3. SPIFFE Helper (`spiffe-helper`) - Sidecar Container

- **Image**: `ghcr.io/spiffe/spiffe-helper:nightly`
- **Purpose**: Obtains and refreshes JWT-SVIDs from SPIRE
- **Resources**: 50m CPU / 64Mi memory (request), 100m CPU / 128Mi memory (limit)
- **Volumes**:
  - `/spiffe-workload-api` - SPIRE agent socket
  - `/etc/spiffe-helper` - Configuration
  - `/opt` - SVID token output

##### 4. Client Registration (`kagenti-client-registration`) - Sidecar Container

- **Image**: `ghcr.io/kagenti/kagenti-extensions/client-registration:latest`
- **Purpose**: Registers resource as Keycloak OAuth2 client using SPIFFE identity
- **Resources**: 50m CPU / 64Mi memory (request), 100m CPU / 128Mi memory (limit)
- **Behavior**: Waits for `/opt/jwt_svid.token`, then registers with Keycloak
- **Volumes**:
  - `/opt` - Reads SVID token from spiffe-helper

#### Legacy Webhook Containers

The legacy Agent CR and MCPServer CR webhooks inject only SPIRE-related sidecars:

- **spiffe-helper** - Obtains JWT-SVIDs from SPIRE
- **kagenti-client-registration** - Registers with Keycloak using SPIFFE identity

### Automatic Volume Configuration

#### AuthBridge Webhook

The AuthBridge webhook automatically adds these volumes:

- **`spire-agent-socket`** - CSI volume (`csi.spiffe.io` driver) providing the SPIRE Workload API socket for SPIRE agent access (when SPIRE enabled)
- **`spiffe-helper-config`** - ConfigMap containing SPIFFE helper configuration (when SPIRE enabled)
- **`svid-output`** - EmptyDir for SVID token exchange between sidecars (when SPIRE enabled)
- **`envoy-config`** - ConfigMap containing Envoy configuration injected by the AuthBridge webhook

#### Legacy Webhooks

The legacy webhooks automatically add these volumes:

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

### Local Development with webhook-rollout.sh

When the webhook is deployed as a **subchart** (e.g., as part of a parent Helm chart), `helm upgrade` on the subchart alone can fail with an immutable `spec.selector` error because the parent chart may use different label selectors. The `webhook-rollout.sh` script works around this by using `kubectl set image` and `kubectl patch` instead of a full Helm upgrade.

The script handles the full build-and-deploy cycle:

1. Builds the Docker image locally
2. Loads the image into the Kind cluster
3. Deploys the platform defaults ConfigMap (`kagenti-webhook-defaults`)
4. Deploys the feature gates ConfigMap (`kagenti-webhook-feature-gates`)
5. Updates the deployment image and patches in config volume mounts
6. Creates the AuthBridge `MutatingWebhookConfiguration` if it doesn't exist

```bash
cd kagenti-webhook

# Basic usage (uses CLUSTER=kagenti by default)
./scripts/webhook-rollout.sh

# Specify cluster and container runtime
CLUSTER=my-cluster ./scripts/webhook-rollout.sh
DOCKER_IMPL=podman ./scripts/webhook-rollout.sh

# Include AuthBridge demo setup (namespace + ConfigMaps)
AUTHBRIDGE_DEMO=true ./scripts/webhook-rollout.sh
AUTHBRIDGE_DEMO=true AUTHBRIDGE_NAMESPACE=myns ./scripts/webhook-rollout.sh
```

Environment variables:

| Variable | Default | Description |
| --- | --- | --- |
| `CLUSTER` | `kagenti` | Kind cluster name |
| `NAMESPACE` | `kagenti-webhook-system` | Webhook deployment namespace |
| `DOCKER_IMPL` | auto-detected | Container runtime (`docker` or `podman`) |
| `AUTHBRIDGE_DEMO` | `false` | Set to `true` to create demo namespace + ConfigMaps |
| `AUTHBRIDGE_NAMESPACE` | `team1` | Namespace for AuthBridge demo workloads |

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

All webhooks use a shared pod mutation engine to eliminate code duplication:

```bash
internal/webhook/
├── config/                          # Configuration layer
│   ├── types.go                     # PlatformConfig, SidecarDefaults types
│   ├── defaults.go                  # Compiled default values
│   ├── loader.go                    # ConfigMap-based config loader with hot-reload
│   ├── feature_gates.go             # FeatureGates type
│   └── feature_gate_loader.go       # Feature gates loader with hot-reload
├── injector/                        # Shared mutation logic
│   ├── pod_mutator.go               # Core mutation engine + InjectAuthBridge
│   ├── precedence.go                # Multi-layer precedence evaluator
│   ├── precedence_test.go           # Table-driven precedence tests
│   ├── injection_decision.go        # SidecarDecision, InjectionDecision types
│   ├── tokenexchange_overrides.go   # TokenExchange CR stub (future use)
│   ├── namespace_checker.go         # Namespace label inspection
│   ├── container_builder.go         # Build sidecars & init containers
│   └── volume_builder.go            # Build volumes
└── v1alpha1/
    ├── authbridge_webhook.go        # AuthBridge webhook (recommended)
    ├── mcpserver_webhook.go         # MCPServer webhook (deprecated)
    └── agent_webhook.go             # Agent webhook (deprecated)
```

All webhooks use the same `PodMutator` instance, ensuring:

- Consistent behavior across resource types
- Single source of truth for injection logic
- Easy to add new resource types or features

The AuthBridge webhook uses the `InjectAuthBridge()` method which supports:

- Init container injection (proxy-init)
- Sidecar container injection (envoy-proxy, spiffe-helper, client-registration)
- Optional SPIRE integration via pod labels
- Support for standard Kubernetes workloads

The legacy webhooks use the `MutatePodSpec()` method which only supports SPIRE-based sidecar injection.

## Migration Guide

### Migrating from Legacy Webhooks to AuthBridge

If you're currently using the legacy Agent CR or MCPServer CR webhooks, you can migrate to the AuthBridge webhook by converting your custom resources to standard Kubernetes workloads:

**Before (Agent CR - Deprecated):**

```yaml
apiVersion: agent.kagenti.dev/v1alpha1
kind: Agent
metadata:
  name: my-agent
  namespace: my-apps
  annotations:
    kagenti.dev/inject: "true"
spec:
  podTemplateSpec:
    spec:
      containers:
      - name: agent
        image: my-agent:latest
```

**After (Deployment with AuthBridge - Recommended):**

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-agent
  namespace: my-apps
spec:
  selector:
    matchLabels:
      app: my-agent
  template:
    metadata:
      labels:
        app: my-agent
        kagenti.io/type: agent       # Required: Identifies workload type (agent or tool)
        kagenti.io/inject: enabled   # Enable injection via pod label
        kagenti.io/spire: enabled    # Enable SPIRE integration
    spec:
      containers:
      - name: agent
        image: my-agent:latest
```

**Key differences:**

- Use **pod labels** instead of CR annotations for injection control
- Add `kagenti.io/type: agent` (or `tool`) label - required for injection to occur
- Add `kagenti.io/spire: enabled` label to enable SPIRE integration (optional)
- AuthBridge also injects `proxy-init` (init container) and `envoy-proxy` (sidecar) for traffic management
- Standard Kubernetes resources benefit from better tooling and ecosystem support


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
