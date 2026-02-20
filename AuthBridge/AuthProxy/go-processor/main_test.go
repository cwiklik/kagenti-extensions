package main

import "testing"

func TestMatchBypassPath(t *testing.T) {
	tests := []struct {
		name         string
		patterns     []string
		requestPath  string
		expectBypass bool
	}{
		{
			name:         "exact match /healthz",
			patterns:     []string{"/healthz", "/readyz"},
			requestPath:  "/healthz",
			expectBypass: true,
		},
		{
			name:         "exact match /readyz",
			patterns:     []string{"/healthz", "/readyz"},
			requestPath:  "/readyz",
			expectBypass: true,
		},
		{
			name:         "glob match /.well-known/*",
			patterns:     []string{"/.well-known/*"},
			requestPath:  "/.well-known/agent.json",
			expectBypass: true,
		},
		{
			name:         "glob does not match nested path",
			patterns:     []string{"/.well-known/*"},
			requestPath:  "/.well-known/a/b",
			expectBypass: false,
		},
		{
			name:         "no match for /api/data",
			patterns:     []string{"/.well-known/*", "/healthz", "/readyz", "/livez"},
			requestPath:  "/api/data",
			expectBypass: false,
		},
		{
			name:         "empty bypass list",
			patterns:     []string{},
			requestPath:  "/healthz",
			expectBypass: false,
		},
		{
			name:         "nil bypass list",
			patterns:     nil,
			requestPath:  "/healthz",
			expectBypass: false,
		},
		{
			name:         "path with query string - matches",
			patterns:     []string{"/healthz"},
			requestPath:  "/healthz?verbose=true",
			expectBypass: true,
		},
		{
			name:         "path with query string - glob matches",
			patterns:     []string{"/.well-known/*"},
			requestPath:  "/.well-known/agent.json?format=json",
			expectBypass: true,
		},
		{
			name:         "path with query string - no match",
			patterns:     []string{"/healthz"},
			requestPath:  "/api/data?healthz=true",
			expectBypass: false,
		},
		{
			name:         "empty request path",
			patterns:     []string{"/healthz"},
			requestPath:  "",
			expectBypass: false,
		},
		{
			name:         "root path exact match",
			patterns:     []string{"/"},
			requestPath:  "/",
			expectBypass: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save and restore the global state
			orig := bypassInboundPaths
			bypassInboundPaths = tt.patterns
			defer func() { bypassInboundPaths = orig }()

			got := matchBypassPath(tt.requestPath)
			if got != tt.expectBypass {
				t.Errorf("matchBypassPath(%q) = %v, want %v (patterns: %v)",
					tt.requestPath, got, tt.expectBypass, tt.patterns)
			}
		})
	}
}

func TestDefaultBypassPaths(t *testing.T) {
	// Verify defaults are applied without any env var override
	orig := bypassInboundPaths
	bypassInboundPaths = defaultBypassInboundPaths
	defer func() { bypassInboundPaths = orig }()

	shouldBypass := []string{
		"/.well-known/agent.json",
		"/.well-known/openid-configuration",
		"/healthz",
		"/readyz",
		"/livez",
	}
	for _, p := range shouldBypass {
		if !matchBypassPath(p) {
			t.Errorf("default bypass should match %q but did not", p)
		}
	}

	shouldBlock := []string{
		"/",
		"/api/data",
		"/v1/tasks",
		"/.well-known/nested/deep",
	}
	for _, p := range shouldBlock {
		if matchBypassPath(p) {
			t.Errorf("default bypass should NOT match %q but did", p)
		}
	}
}
