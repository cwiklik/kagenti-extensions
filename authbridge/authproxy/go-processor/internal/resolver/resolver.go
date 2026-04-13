// Package resolver provides abstractions for mapping destination hosts
// to token exchange configuration.
package resolver

import "context"

// TargetConfig describes the token exchange parameters for a target service.
// We use "target" terminology deliberately - these are resource servers that
// receive tokens, not OAuth clients that request them.
type TargetConfig struct {
	// Audience identifies the target resource server.
	// This becomes the "aud" claim in the exchanged token.
	Audience string

	// Scopes are the permissions to request in the exchanged token.
	Scopes string

	// TokenEndpoint overrides the default token endpoint for this target.
	// If empty, the global token endpoint is used.
	TokenEndpoint string

	// Passthrough skips token exchange entirely.
	// Use for trusted internal services that don't need exchange.
	Passthrough bool
}

// TargetResolver maps a destination host to its token exchange configuration.
// Implementations may use static configuration, IDP lookups, or other strategies.
type TargetResolver interface {
	// Resolve returns the exchange configuration for the given host.
	// Returns nil (not error) if no specific configuration exists,
	// in which case the caller should use default/global configuration.
	Resolve(ctx context.Context, host string) (*TargetConfig, error)
}
