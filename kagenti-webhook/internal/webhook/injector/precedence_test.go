package injector

import (
	"testing"

	"github.com/kagenti/kagenti-extensions/kagenti-webhook/internal/webhook/config"
	"k8s.io/utils/ptr"
)

func allEnabledGates() *config.FeatureGates {
	return config.DefaultFeatureGates()
}

func allEnabledConfig() *config.PlatformConfig {
	return config.CompiledDefaults()
}

func optedInNamespace() map[string]string {
	return map[string]string{LabelNamespaceInject: "true"}
}

func noLabels() map[string]string {
	return map[string]string{}
}

func spireEnabled() map[string]string {
	return map[string]string{SpireEnableLabel: SpireEnabledValue}
}

func TestPrecedenceEvaluator(t *testing.T) {
	tests := []struct {
		name                   string
		featureGates           *config.FeatureGates
		platformConfig         *config.PlatformConfig
		namespaceLabels        map[string]string
		workloadLabels         map[string]string
		tokenExchangeOverrides *TokenExchangeOverrides
		expectEnvoy            bool
		expectProxyInit        bool
		expectSpiffe           bool
		expectClientReg        bool
		expectEnvoyLayer       string
	}{
		// === Global feature gate tests ===
		{
			name: "global kill switch off - all skipped",
			featureGates: &config.FeatureGates{
				GlobalEnabled:      false,
				EnvoyProxy:         true,
				SpiffeHelper:       true,
				ClientRegistration: true,
			},
			platformConfig:   allEnabledConfig(),
			namespaceLabels:  optedInNamespace(),
			workloadLabels:   noLabels(),
			expectEnvoy:      false,
			expectProxyInit:  false,
			expectSpiffe:     false,
			expectClientReg:  false,
			expectEnvoyLayer: "global-gate",
		},
		{
			name: "per-sidecar gate off - only envoy skipped",
			featureGates: &config.FeatureGates{
				GlobalEnabled:      true,
				EnvoyProxy:         false,
				SpiffeHelper:       true,
				ClientRegistration: true,
			},
			platformConfig:   allEnabledConfig(),
			namespaceLabels:  optedInNamespace(),
			workloadLabels:   spireEnabled(),
			expectEnvoy:      false,
			expectProxyInit:  false, // follows envoy
			expectSpiffe:     true,
			expectClientReg:  true,
			expectEnvoyLayer: "feature-gate",
		},
		{
			name: "per-sidecar gate off - spiffe skipped",
			featureGates: &config.FeatureGates{
				GlobalEnabled:      true,
				EnvoyProxy:         true,
				SpiffeHelper:       false,
				ClientRegistration: true,
			},
			platformConfig:  allEnabledConfig(),
			namespaceLabels: optedInNamespace(),
			workloadLabels:  spireEnabled(),
			expectEnvoy:     true,
			expectProxyInit: true,
			expectSpiffe:    false,
			expectClientReg: true,
		},

		// === Namespace tests ===
		{
			name:             "namespace not opted in - all skipped",
			featureGates:     allEnabledGates(),
			platformConfig:   allEnabledConfig(),
			namespaceLabels:  noLabels(),
			workloadLabels:   noLabels(),
			expectEnvoy:      false,
			expectProxyInit:  false,
			expectSpiffe:     false,
			expectClientReg:  false,
			expectEnvoyLayer: "namespace",
		},
		{
			name:            "namespace opted in - proceed to next layer",
			featureGates:    allEnabledGates(),
			platformConfig:  allEnabledConfig(),
			namespaceLabels: optedInNamespace(),
			workloadLabels:  spireEnabled(),
			expectEnvoy:     true,
			expectProxyInit: true,
			expectSpiffe:    true,
			expectClientReg: true,
		},
		{
			name:             "namespace label wrong value - all skipped",
			featureGates:     allEnabledGates(),
			platformConfig:   allEnabledConfig(),
			namespaceLabels:  map[string]string{LabelNamespaceInject: "false"},
			workloadLabels:   noLabels(),
			expectEnvoy:      false,
			expectProxyInit:  false,
			expectSpiffe:     false,
			expectClientReg:  false,
			expectEnvoyLayer: "namespace",
		},

		// === Workload label tests ===
		{
			name:             "workload label disables envoy - envoy and proxy-init skipped",
			featureGates:     allEnabledGates(),
			platformConfig:   allEnabledConfig(),
			namespaceLabels:  optedInNamespace(),
			workloadLabels:   map[string]string{LabelEnvoyProxyInject: "false", SpireEnableLabel: SpireEnabledValue},
			expectEnvoy:      false,
			expectProxyInit:  false,
			expectSpiffe:     true,
			expectClientReg:  true,
			expectEnvoyLayer: "workload-label",
		},
		{
			name:            "workload label disables spiffe only",
			featureGates:    allEnabledGates(),
			platformConfig:  allEnabledConfig(),
			namespaceLabels: optedInNamespace(),
			workloadLabels:  map[string]string{LabelSpiffeHelperInject: "false", SpireEnableLabel: SpireEnabledValue},
			expectEnvoy:     true,
			expectProxyInit: true,
			expectSpiffe:    false,
			expectClientReg: true,
		},
		{
			name:            "workload label disables client-registration only",
			featureGates:    allEnabledGates(),
			platformConfig:  allEnabledConfig(),
			namespaceLabels: optedInNamespace(),
			workloadLabels:  map[string]string{LabelClientRegistrationInject: "false", SpireEnableLabel: SpireEnabledValue},
			expectEnvoy:     true,
			expectProxyInit: true,
			expectSpiffe:    true,
			expectClientReg: false,
		},
		{
			name:            "workload label true - no effect (default is inject)",
			featureGates:    allEnabledGates(),
			platformConfig:  allEnabledConfig(),
			namespaceLabels: optedInNamespace(),
			workloadLabels:  map[string]string{LabelEnvoyProxyInject: "true", SpireEnableLabel: SpireEnabledValue},
			expectEnvoy:     true,
			expectProxyInit: true,
			expectSpiffe:    true,
			expectClientReg: true,
		},
		{
			name:            "workload label absent - no effect",
			featureGates:    allEnabledGates(),
			platformConfig:  allEnabledConfig(),
			namespaceLabels: optedInNamespace(),
			workloadLabels:  spireEnabled(),
			expectEnvoy:     true,
			expectProxyInit: true,
			expectSpiffe:    true,
			expectClientReg: true,
		},

		// === TokenExchange CRD tests ===
		{
			name:                   "CRD overrides nil - no effect",
			featureGates:           allEnabledGates(),
			platformConfig:         allEnabledConfig(),
			namespaceLabels:        optedInNamespace(),
			workloadLabels:         spireEnabled(),
			tokenExchangeOverrides: nil,
			expectEnvoy:            true,
			expectProxyInit:        true,
			expectSpiffe:           true,
			expectClientReg:        true,
		},
		{
			name:            "CRD explicitly disables envoy",
			featureGates:    allEnabledGates(),
			platformConfig:  allEnabledConfig(),
			namespaceLabels: optedInNamespace(),
			workloadLabels:  spireEnabled(),
			tokenExchangeOverrides: &TokenExchangeOverrides{
				EnvoyProxy: ptr.To(false),
			},
			expectEnvoy:      false,
			expectProxyInit:  false,
			expectSpiffe:     true,
			expectClientReg:  true,
			expectEnvoyLayer: "tokenexchange-cr",
		},
		{
			name: "CRD enables sidecar that higher layer disabled - still skipped",
			featureGates: &config.FeatureGates{
				GlobalEnabled:      true,
				EnvoyProxy:         false, // higher layer disables
				SpiffeHelper:       true,
				ClientRegistration: true,
			},
			platformConfig:  allEnabledConfig(),
			namespaceLabels: optedInNamespace(),
			workloadLabels:  spireEnabled(),
			tokenExchangeOverrides: &TokenExchangeOverrides{
				EnvoyProxy: ptr.To(true), // CRD tries to enable — should NOT override
			},
			expectEnvoy:      false,
			expectProxyInit:  false,
			expectSpiffe:     true,
			expectClientReg:  true,
			expectEnvoyLayer: "feature-gate",
		},
		{
			name:         "CRD enables sidecar that platform default disabled - injected (CRD wins over platform)",
			featureGates: allEnabledGates(),
			platformConfig: func() *config.PlatformConfig {
				c := allEnabledConfig()
				c.Sidecars.ClientRegistration.Enabled = false // platform default disables
				return c
			}(),
			namespaceLabels: optedInNamespace(),
			workloadLabels:  spireEnabled(),
			tokenExchangeOverrides: &TokenExchangeOverrides{
				ClientRegistration: ptr.To(true), // CRD explicitly enables — should override platform default
			},
			expectEnvoy:      true,
			expectProxyInit:  true,
			expectSpiffe:     true,
			expectClientReg:  true, // CRD wins over platform default
			expectEnvoyLayer: "default",
		},

		// === Platform defaults tests ===
		{
			name:         "platform default disables envoy",
			featureGates: allEnabledGates(),
			platformConfig: func() *config.PlatformConfig {
				c := allEnabledConfig()
				c.Sidecars.EnvoyProxy.Enabled = false
				return c
			}(),
			namespaceLabels:  optedInNamespace(),
			workloadLabels:   spireEnabled(),
			expectEnvoy:      false,
			expectProxyInit:  false,
			expectSpiffe:     true,
			expectClientReg:  true,
			expectEnvoyLayer: "platform-default",
		},
		{
			name:         "platform default disables all sidecars",
			featureGates: allEnabledGates(),
			platformConfig: func() *config.PlatformConfig {
				c := allEnabledConfig()
				c.Sidecars.EnvoyProxy.Enabled = false
				c.Sidecars.SpiffeHelper.Enabled = false
				c.Sidecars.ClientRegistration.Enabled = false
				return c
			}(),
			namespaceLabels: optedInNamespace(),
			workloadLabels:  noLabels(),
			expectEnvoy:     false,
			expectProxyInit: false,
			expectSpiffe:    false,
			expectClientReg: false,
		},

		// === Precedence ordering tests ===
		{
			name: "global gate off + workload label enables - skipped (global wins)",
			featureGates: &config.FeatureGates{
				GlobalEnabled:      false,
				EnvoyProxy:         true,
				SpiffeHelper:       true,
				ClientRegistration: true,
			},
			platformConfig:   allEnabledConfig(),
			namespaceLabels:  optedInNamespace(),
			workloadLabels:   map[string]string{LabelEnvoyProxyInject: "true"},
			expectEnvoy:      false,
			expectProxyInit:  false,
			expectSpiffe:     false,
			expectClientReg:  false,
			expectEnvoyLayer: "global-gate",
		},
		{
			name:            "namespace opted in + workload label disables - skipped (workload wins over namespace)",
			featureGates:    allEnabledGates(),
			platformConfig:  allEnabledConfig(),
			namespaceLabels: optedInNamespace(),
			workloadLabels: map[string]string{
				LabelEnvoyProxyInject:         "false",
				LabelSpiffeHelperInject:       "false",
				LabelClientRegistrationInject: "false",
			},
			expectEnvoy:      false,
			expectProxyInit:  false,
			expectSpiffe:     false,
			expectClientReg:  false,
			expectEnvoyLayer: "workload-label",
		},
		{
			name:            "CRD disables + platform enables - skipped (CRD wins over platform)",
			featureGates:    allEnabledGates(),
			platformConfig:  allEnabledConfig(),
			namespaceLabels: optedInNamespace(),
			workloadLabels:  noLabels(),
			tokenExchangeOverrides: &TokenExchangeOverrides{
				SpiffeHelper: ptr.To(false),
			},
			expectEnvoy:     true,
			expectProxyInit: true,
			expectSpiffe:    false,
			expectClientReg: true,
		},
		{
			name:             "all layers pass - all injected",
			featureGates:     allEnabledGates(),
			platformConfig:   allEnabledConfig(),
			namespaceLabels:  optedInNamespace(),
			workloadLabels:   spireEnabled(),
			expectEnvoy:      true,
			expectProxyInit:  true,
			expectSpiffe:     true,
			expectClientReg:  true,
			expectEnvoyLayer: "default",
		},

		// === proxy-init coupling tests ===
		{
			name:            "envoy injected - proxy-init injected",
			featureGates:    allEnabledGates(),
			platformConfig:  allEnabledConfig(),
			namespaceLabels: optedInNamespace(),
			workloadLabels:  spireEnabled(),
			expectEnvoy:     true,
			expectProxyInit: true,
			expectSpiffe:    true,
			expectClientReg: true,
		},
		{
			name:         "envoy skipped via platform default - proxy-init also skipped",
			featureGates: allEnabledGates(),
			platformConfig: func() *config.PlatformConfig {
				c := allEnabledConfig()
				c.Sidecars.EnvoyProxy.Enabled = false
				return c
			}(),
			namespaceLabels: optedInNamespace(),
			workloadLabels:  spireEnabled(),
			expectEnvoy:     false,
			expectProxyInit: false,
			expectSpiffe:    true,
			expectClientReg: true,
		},
		{
			name:            "envoy skipped via workload label - proxy-init also skipped",
			featureGates:    allEnabledGates(),
			platformConfig:  allEnabledConfig(),
			namespaceLabels: optedInNamespace(),
			workloadLabels:  map[string]string{LabelEnvoyProxyInject: "false", SpireEnableLabel: SpireEnabledValue},
			expectEnvoy:     false,
			expectProxyInit: false,
			expectSpiffe:    true,
			expectClientReg: true,
		},

		// === SPIRE label opt-out tests (spiffe-helper only) ===
		{
			// No SPIRE label: spiffe-helper is injected by default (opt-out model).
			name:            "SPIRE label missing - spiffe-helper injected",
			featureGates:    allEnabledGates(),
			platformConfig:  allEnabledConfig(),
			namespaceLabels: optedInNamespace(),
			workloadLabels:  noLabels(),
			expectEnvoy:     true,
			expectProxyInit: true,
			expectSpiffe:    true,
			expectClientReg: true,
		},
		{
			// kagenti.io/spire=disabled is the explicit opt-out mechanism.
			name:            "SPIRE label disabled - spiffe-helper skipped",
			featureGates:    allEnabledGates(),
			platformConfig:  allEnabledConfig(),
			namespaceLabels: optedInNamespace(),
			workloadLabels:  map[string]string{SpireEnableLabel: SpireDisabledValue},
			expectEnvoy:     true,
			expectProxyInit: true,
			expectSpiffe:    false,
			expectClientReg: true,
		},
		{
			// Only kagenti.io/spire=disabled blocks; any other value (incl. "enabled") does not.
			name:            "SPIRE label set to non-disabled value - spiffe-helper injected",
			featureGates:    allEnabledGates(),
			platformConfig:  allEnabledConfig(),
			namespaceLabels: optedInNamespace(),
			workloadLabels:  spireEnabled(),
			expectEnvoy:     true,
			expectProxyInit: true,
			expectSpiffe:    true,
			expectClientReg: true,
		},
		{
			name: "SPIRE label absent but precedence chain blocks - spiffe-helper still skipped",
			featureGates: &config.FeatureGates{
				GlobalEnabled:      true,
				EnvoyProxy:         true,
				SpiffeHelper:       false, // blocked at feature gate
				ClientRegistration: true,
			},
			platformConfig:  allEnabledConfig(),
			namespaceLabels: optedInNamespace(),
			workloadLabels:  noLabels(),
			expectEnvoy:     true,
			expectProxyInit: true,
			expectSpiffe:    false,
			expectClientReg: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evaluator := NewPrecedenceEvaluator(tt.featureGates, tt.platformConfig)
			decision := evaluator.Evaluate(tt.namespaceLabels, tt.workloadLabels, tt.tokenExchangeOverrides)

			if decision.EnvoyProxy.Inject != tt.expectEnvoy {
				t.Errorf("EnvoyProxy.Inject = %v, want %v (reason: %s, layer: %s)",
					decision.EnvoyProxy.Inject, tt.expectEnvoy,
					decision.EnvoyProxy.Reason, decision.EnvoyProxy.Layer)
			}
			if decision.ProxyInit.Inject != tt.expectProxyInit {
				t.Errorf("ProxyInit.Inject = %v, want %v (reason: %s, layer: %s)",
					decision.ProxyInit.Inject, tt.expectProxyInit,
					decision.ProxyInit.Reason, decision.ProxyInit.Layer)
			}
			if decision.SpiffeHelper.Inject != tt.expectSpiffe {
				t.Errorf("SpiffeHelper.Inject = %v, want %v (reason: %s, layer: %s)",
					decision.SpiffeHelper.Inject, tt.expectSpiffe,
					decision.SpiffeHelper.Reason, decision.SpiffeHelper.Layer)
			}
			if decision.ClientRegistration.Inject != tt.expectClientReg {
				t.Errorf("ClientRegistration.Inject = %v, want %v (reason: %s, layer: %s)",
					decision.ClientRegistration.Inject, tt.expectClientReg,
					decision.ClientRegistration.Reason, decision.ClientRegistration.Layer)
			}
			if tt.expectEnvoyLayer != "" && decision.EnvoyProxy.Layer != tt.expectEnvoyLayer {
				t.Errorf("EnvoyProxy.Layer = %q, want %q", decision.EnvoyProxy.Layer, tt.expectEnvoyLayer)
			}
		})
	}
}

func TestAnyInjected(t *testing.T) {
	tests := []struct {
		name     string
		decision InjectionDecision
		want     bool
	}{
		{
			name: "all injected",
			decision: InjectionDecision{
				EnvoyProxy:         SidecarDecision{Inject: true},
				SpiffeHelper:       SidecarDecision{Inject: true},
				ClientRegistration: SidecarDecision{Inject: true},
			},
			want: true,
		},
		{
			name: "only envoy injected",
			decision: InjectionDecision{
				EnvoyProxy:         SidecarDecision{Inject: true},
				SpiffeHelper:       SidecarDecision{Inject: false},
				ClientRegistration: SidecarDecision{Inject: false},
			},
			want: true,
		},
		{
			name: "none injected",
			decision: InjectionDecision{
				EnvoyProxy:         SidecarDecision{Inject: false},
				SpiffeHelper:       SidecarDecision{Inject: false},
				ClientRegistration: SidecarDecision{Inject: false},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.decision.AnyInjected(); got != tt.want {
				t.Errorf("AnyInjected() = %v, want %v", got, tt.want)
			}
		})
	}
}
