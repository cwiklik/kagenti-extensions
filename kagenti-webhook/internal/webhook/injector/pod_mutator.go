/*
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
*/

package injector

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var mutatorLog = logf.Log.WithName("pod-mutator")

const (
	// Container names
	SpiffeHelperContainerName       = "spiffe-helper"
	ClientRegistrationContainerName = "kagenti-client-registration"

	// Default configuration
	DefaultNamespaceLabel      = "kagenti-enabled"
	DefaultNamespaceAnnotation = "kagenti.dev/inject"
	DefaultCRAnnotation        = "kagenti.dev/inject"
	// Label selector for authbridge injection
	AuthBridgeInjectLabel   = "kagenti.io/inject"
	AuthBridgeInjectValue   = "enabled"
	AuthBridgeDisabledValue = "disabled"
)

type PodMutator struct {
	Client                   client.Client
	EnableClientRegistration bool
	NamespaceLabel           string
	NamespaceAnnotation      string
}

func NewPodMutator(client client.Client, enableClientRegistration bool) *PodMutator {
	return &PodMutator{
		Client:                   client,
		EnableClientRegistration: enableClientRegistration,
		NamespaceLabel:           DefaultNamespaceLabel,
		NamespaceAnnotation:      DefaultNamespaceAnnotation,
	}
}

// DEPRECATED, used by Agent and MCPServer CRs. Remove ShouldMutate after both CRs are deleted and use InjectAuthBridge instead.

// main entry point for pod mutations
// It checks if injection should occur and performs all necessary mutations
func (m *PodMutator) MutatePodSpec(ctx context.Context, podSpec *corev1.PodSpec, namespace, crName string, crAnnotations map[string]string) error {
	mutatorLog.Info("MutatePodSpec called", "namespace", namespace, "crName", crName, "annotations", crAnnotations)

	shouldMutate, err := m.ShouldMutate(ctx, namespace, crAnnotations)
	if err != nil {
		mutatorLog.Error(err, "Failed to determine if mutation should occur", "namespace", namespace, "crName", crName)
		return fmt.Errorf("failed to determine if mutation should occur: %w", err)
	}

	if !shouldMutate {
		mutatorLog.Info("Skipping mutation (injection not enabled)", "namespace", namespace, "crName", crName)
		return nil // Skip mutation
	}

	mutatorLog.Info("Mutation enabled - injecting sidecars and volumes", "namespace", namespace, "crName", crName)

	if err := m.InjectSidecars(podSpec, namespace, crName); err != nil {
		mutatorLog.Error(err, "Failed to inject sidecars", "namespace", namespace, "crName", crName)
		return fmt.Errorf("failed to inject sidecars: %w", err)
	}

	if err := m.InjectVolumes(podSpec); err != nil {
		mutatorLog.Error(err, "Failed to inject volumes", "namespace", namespace, "crName", crName)
		return fmt.Errorf("failed to inject volumes: %w", err)
	}

	mutatorLog.Info("Successfully mutated pod spec", "namespace", namespace, "crName", crName, "containers", len(podSpec.Containers), "volumes", len(podSpec.Volumes))
	return nil
}

// It checks if injection should occur and performs all necessary mutations
func (m *PodMutator) InjectAuthBridge(ctx context.Context, podSpec *corev1.PodSpec, namespace, crName string, labels map[string]string) (bool, error) {
	mutatorLog.Info("InjectAuthBridge called", "namespace", namespace, "crName", crName, "labels", labels)

	shouldMutate, err := m.NeedsMutation(ctx, namespace, labels)
	if err != nil {
		mutatorLog.Error(err, "Failed to determine if mutation should occur", "namespace", namespace, "crName", crName)
		return false, fmt.Errorf("failed to determine if mutation should occur: %w", err)
	}

	if !shouldMutate {
		mutatorLog.Info("Skipping mutation (injection not enabled)", "namespace", namespace, "crName", crName)
		return false, nil // Skip mutation
	}

	mutatorLog.Info("Mutation enabled - injecting sidecars and volumes", "namespace", namespace, "crName", crName)

	if err := m.InjectSidecars(podSpec, namespace, crName); err != nil {
		mutatorLog.Error(err, "Failed to inject sidecars", "namespace", namespace, "crName", crName)
		return false, fmt.Errorf("failed to inject sidecars: %w", err)
	}

	if err := m.InjectVolumes(podSpec); err != nil {
		mutatorLog.Error(err, "Failed to inject volumes", "namespace", namespace, "crName", crName)
		return false, fmt.Errorf("failed to inject volumes: %w", err)
	}

	mutatorLog.Info("Successfully mutated pod spec", "namespace", namespace, "crName", crName, "containers", len(podSpec.Containers), "volumes", len(podSpec.Volumes))
	return true, nil
}

// DEPRECATED, used by Agent and MCPServer CRs. Remove ShouldMutate after both CRs are deleted and use NeedsMutation instead.

// determines if pod mutation should occur based on annotations and namespace labels
// Priority order:
// 1. CR annotation (opt-out): kagenti.dev/inject=false
// 2. CR annotation (opt-in): kagenti.dev/inject=true
// 3. Namespace label: kagenti-enabled=true
// 4. Namespace annotation: kagenti.dev/inject=true

func (m *PodMutator) ShouldMutate(ctx context.Context, namespace string, crAnnotations map[string]string) (bool, error) {
	mutatorLog.Info("Checking if mutation should occur", "namespace", namespace, "crAnnotations", crAnnotations)

	// Priority 1: CR-level opt-out (explicit disable)
	if crAnnotations[DefaultCRAnnotation] == "false" {
		mutatorLog.Info("CR annotation opt-out detected", "namespace", namespace, "annotation", DefaultCRAnnotation)
		return false, nil
	}

	// Priority 2: CR-level opt-in (explicit enable)
	if crAnnotations[DefaultCRAnnotation] == "true" {
		mutatorLog.Info("CR annotation opt-in detected", "namespace", namespace, "annotation", DefaultCRAnnotation)
		return true, nil
	}

	// Priority 3 & 4: Check namespace-level settings
	mutatorLog.Info("Checking namespace-level injection settings", "namespace", namespace, "label", m.NamespaceLabel, "annotation", m.NamespaceAnnotation)
	nsInjectionEnabled, err := CheckNamespaceInjectionEnabled(ctx, m.Client, namespace, m.NamespaceLabel, m.NamespaceAnnotation)
	if err != nil {
		mutatorLog.Error(err, "Failed to check namespace injection settings", "namespace", namespace)
		return false, fmt.Errorf("failed to check namespace injection settings: %w", err)
	}

	if nsInjectionEnabled {
		mutatorLog.Info("Namespace-level injection enabled", "namespace", namespace)
		return true, nil
	}
	return false, nil
}
func (m *PodMutator) NeedsMutation(ctx context.Context, namespace string, labels map[string]string) (bool, error) {
	mutatorLog.Info("Checking if mutation should occur", "namespace", namespace, "labels", labels)

	value, exists := labels[AuthBridgeInjectLabel]

	// If label exists, respect its value (opt-in or opt-out)
	if exists {
		if value == AuthBridgeInjectValue {
			mutatorLog.Info("Workload label opt-in detected ")
			return true, nil
		}
		// Any other value (including "disabled", "false", etc.) is opt-out
		mutatorLog.Info("Workload label opt-out detected ")
		return false, nil
	}

	// No label - fall back to namespace-level settings
	mutatorLog.Info("Checking namespace-level injection settings", "namespace", namespace, "label", m.NamespaceLabel)
	return IsNamespaceInjectionEnabled(ctx, m.Client, namespace, m.NamespaceLabel)
}
func (m *PodMutator) InjectSidecars(podSpec *corev1.PodSpec, namespace, crName string) error {
	if podSpec.Containers == nil {
		podSpec.Containers = []corev1.Container{}
	}

	// Check and inject spiffe-helper sidecar
	if !containerExists(podSpec.Containers, SpiffeHelperContainerName) {
		podSpec.Containers = append(podSpec.Containers, BuildSpiffeHelperContainer())
	}

	// Check and inject client-registration sidecar
	if !containerExists(podSpec.Containers, ClientRegistrationContainerName) {
		clientID := fmt.Sprintf("%s/%s", namespace, crName)
		podSpec.Containers = append(podSpec.Containers, BuildClientRegistrationContainer(clientID, crName, namespace))
	}

	return nil
}

func (m *PodMutator) InjectVolumes(podSpec *corev1.PodSpec) error {
	mutatorLog.Info("Injecting volumes", "existingVolumes", len(podSpec.Volumes))

	if podSpec.Volumes == nil {
		podSpec.Volumes = []corev1.Volume{}
	}

	// Add all required volumes if they don't exist
	requiredVolumes := BuildRequiredVolumes()
	injectedCount := 0
	for _, vol := range requiredVolumes {
		if !volumeExists(podSpec.Volumes, vol.Name) {
			mutatorLog.Info("Injecting volume", "volumeName", vol.Name)
			podSpec.Volumes = append(podSpec.Volumes, vol)
			injectedCount++
		}
	}

	mutatorLog.Info("Volume injection complete", "totalVolumes", len(podSpec.Volumes), "injected", injectedCount)
	return nil
}

func containerExists(containers []corev1.Container, name string) bool {
	for _, container := range containers {
		if container.Name == name {
			return true
		}
	}
	return false
}

func volumeExists(volumes []corev1.Volume, name string) bool {
	for _, vol := range volumes {
		if vol.Name == name {
			return true
		}
	}
	return false
}
