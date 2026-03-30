package injector

import (
	"testing"

	"github.com/kagenti/kagenti-extensions/kagenti-webhook/internal/webhook/config"
)

func allEnabledGates() *config.FeatureGates {
	return config.DefaultFeatureGates()
}

func allEnabledConfig() *config.PlatformConfig {
	return config.CompiledDefaults()
}

func noLabels() map[string]string {
	return map[string]string{}
}

func TestPrecedenceEvaluator(t *testing.T) {
	tests := []struct {
		name             string
		featureGates     *config.FeatureGates
		platformConfig   *config.PlatformConfig
		workloadLabels   map[string]string
		expectEnvoy      bool
		expectProxyInit  bool
		expectSpiffe     bool
		expectClientReg  bool
		expectEnvoyLayer string
	}{
		// === Per-sidecar feature gate tests ===
		{
			name: "per-sidecar gate off - envoy skipped",
			featureGates: &config.FeatureGates{
				GlobalEnabled:      true,
				EnvoyProxy:         false,
				SpiffeHelper:       true,
				ClientRegistration: true,
			},
			platformConfig:   allEnabledConfig(),
			workloadLabels:   noLabels(),
			expectEnvoy:      false,
			expectProxyInit:  false, // follows envoy
			expectSpiffe:     true,
			expectClientReg:  false,
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
			workloadLabels:  noLabels(),
			expectEnvoy:     true,
			expectProxyInit: true,
			expectSpiffe:    false,
			expectClientReg: false,
		},
		{
			name: "per-sidecar gate off - client-registration skipped",
			featureGates: &config.FeatureGates{
				GlobalEnabled:      true,
				EnvoyProxy:         true,
				SpiffeHelper:       true,
				ClientRegistration: false,
			},
			platformConfig:  allEnabledConfig(),
			workloadLabels:  noLabels(),
			expectEnvoy:     true,
			expectProxyInit: true,
			expectSpiffe:    true,
			expectClientReg: false,
		},

		// === Workload label opt-out tests ===
		{
			name:             "workload label disables envoy - envoy and proxy-init skipped",
			featureGates:     allEnabledGates(),
			platformConfig:   allEnabledConfig(),
			workloadLabels:   map[string]string{LabelEnvoyProxyInject: "false"},
			expectEnvoy:      false,
			expectProxyInit:  false,
			expectSpiffe:     true,
			expectClientReg:  false,
			expectEnvoyLayer: "workload-label",
		},
		{
			name:            "workload label disables spiffe only",
			featureGates:    allEnabledGates(),
			platformConfig:  allEnabledConfig(),
			workloadLabels:  map[string]string{LabelSpiffeHelperInject: "false"},
			expectEnvoy:     true,
			expectProxyInit: true,
			expectSpiffe:    false,
			expectClientReg: false,
		},
		{
			name:            "workload label false - no legacy client-registration (default)",
			featureGates:    allEnabledGates(),
			platformConfig:  allEnabledConfig(),
			workloadLabels:  map[string]string{LabelClientRegistrationInject: "false"},
			expectEnvoy:     true,
			expectProxyInit: true,
			expectSpiffe:    true,
			expectClientReg: false,
		},
		{
			name:            "workload label true - legacy client-registration opted in",
			featureGates:    allEnabledGates(),
			platformConfig:  allEnabledConfig(),
			workloadLabels:  map[string]string{LabelClientRegistrationInject: "true"},
			expectEnvoy:     true,
			expectProxyInit: true,
			expectSpiffe:    true,
			expectClientReg: true,
		},
		{
			name:            "workload label true on envoy - no effect on envoy; client-reg still needs own label",
			featureGates:    allEnabledGates(),
			platformConfig:  allEnabledConfig(),
			workloadLabels:  map[string]string{LabelEnvoyProxyInject: "true"},
			expectEnvoy:     true,
			expectProxyInit: true,
			expectSpiffe:    true,
			expectClientReg: false,
		},
		{
			name:            "workload labels absent - envoy/spiffe default; no legacy client-registration",
			featureGates:    allEnabledGates(),
			platformConfig:  allEnabledConfig(),
			workloadLabels:  noLabels(),
			expectEnvoy:     true,
			expectProxyInit: true,
			expectSpiffe:    true,
			expectClientReg: false,
		},
		{
			name:           "all workload opt-out labels set - all skipped",
			featureGates:   allEnabledGates(),
			platformConfig: allEnabledConfig(),
			workloadLabels: map[string]string{
				LabelEnvoyProxyInject:         "false",
				LabelSpiffeHelperInject:       "false",
				LabelClientRegistrationInject: "false",
			},
			expectEnvoy:      false,
			expectProxyInit:  false,
			expectSpiffe:     false,
			expectClientReg:  false, // client-registration false label same as default (no legacy sidecar)
			expectEnvoyLayer: "workload-label",
		},

		// === Precedence ordering: feature gate beats workload label ===
		{
			name: "feature gate off + workload label absent - skipped (gate wins)",
			featureGates: &config.FeatureGates{
				GlobalEnabled:      true,
				EnvoyProxy:         false,
				SpiffeHelper:       true,
				ClientRegistration: true,
			},
			platformConfig:   allEnabledConfig(),
			workloadLabels:   map[string]string{LabelEnvoyProxyInject: "true"},
			expectEnvoy:      false,
			expectProxyInit:  false,
			expectSpiffe:     true,
			expectClientReg:  false,
			expectEnvoyLayer: "feature-gate",
		},
		{
			name:             "all gates pass - envoy/spiffe injected; client-registration is opt-in",
			featureGates:     allEnabledGates(),
			platformConfig:   allEnabledConfig(),
			workloadLabels:   noLabels(),
			expectEnvoy:      true,
			expectProxyInit:  true,
			expectSpiffe:     true,
			expectClientReg:  false,
			expectEnvoyLayer: "default",
		},

		// === proxy-init coupling tests ===
		{
			name: "envoy skipped via feature gate - proxy-init also skipped",
			featureGates: &config.FeatureGates{
				GlobalEnabled:      true,
				EnvoyProxy:         false,
				SpiffeHelper:       true,
				ClientRegistration: true,
			},
			platformConfig:  allEnabledConfig(),
			workloadLabels:  noLabels(),
			expectEnvoy:     false,
			expectProxyInit: false,
			expectSpiffe:    true,
			expectClientReg: false,
		},
		{
			name:            "envoy skipped via workload label - proxy-init also skipped",
			featureGates:    allEnabledGates(),
			platformConfig:  allEnabledConfig(),
			workloadLabels:  map[string]string{LabelEnvoyProxyInject: "false"},
			expectEnvoy:     false,
			expectProxyInit: false,
			expectSpiffe:    true,
			expectClientReg: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evaluator := NewPrecedenceEvaluator(tt.featureGates, tt.platformConfig)
			decision := evaluator.Evaluate(tt.workloadLabels)

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
