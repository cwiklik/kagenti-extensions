# Kagenti Extensions

This repository contains extension projects for the Kagenti ecosystem

## Projects

- **kagenti-webhook** — Migrated to [kagenti/kagenti-operator](https://github.com/kagenti/kagenti-operator). See [kagenti-operator#238](https://github.com/kagenti/kagenti-operator/issues/238) for details.
- [AuthBridge](./authbridge/) - Collection of Identity components to demonstrate a complete end-to-end authentication flow with [SPIFFE/SPIRE](https://spiffe.io) integration
  - [AuthProxy](./authbridge/authproxy/) - AuthProxy is a **JWT validation and token exchange proxy** for Kubernetes workloads. It enables secure service-to-service communication by intercepting and validating incoming tokens and transparently exchanging them for tokens with the correct audience for downstream services.
  - [Keycloak client-registration](./authbridge/client-registration/) - Keycloak Client Registration is an **automated OAuth2/OIDC client provisioning** tool for Kubernetes workloads. It automatically registers pods as Keycloak clients, eliminating the need for manual client configuration and static credentials.
