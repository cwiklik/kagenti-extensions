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

package v1alpha1

import (
	"context"
	"fmt"

	"github.com/kagenti/kagenti-extensions/kagenti-webhook/internal/webhook/injector"
	agentsv1alpha1 "github.com/kagenti/operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// nolint:unused
// log is for logging in this package.
var agentlog = logf.Log.WithName("agent-resource")

// SetupAgentWebhookWithManager registers the webhook for Agent in the manager.
func SetupAgentWebhookWithManager(mgr ctrl.Manager, mutator *injector.PodMutator) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(&agentsv1alpha1.Agent{}).
		WithValidator(&AgentCustomValidator{}).
		WithDefaulter(&AgentCustomDefaulter{Mutator: mutator}).
		Complete()
}

// +kubebuilder:webhook:path=/mutate-agent-kagenti-dev-v1alpha1-agent,mutating=true,failurePolicy=fail,sideEffects=None,groups=agent.kagenti.dev,resources=agents,verbs=create;update,versions=v1alpha1,name=magent-v1alpha1.kb.io,admissionReviewVersions=v1

// AgentCustomDefaulter struct is responsible for setting default values on the custom resource of the
// Kind Agent when those are created or updated.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as it is used only for temporary operations and does not need to be deeply copied.
type AgentCustomDefaulter struct {
	Mutator *injector.PodMutator
}

var _ webhook.CustomDefaulter = &AgentCustomDefaulter{}

// Default implements webhook.CustomDefaulter so a webhook will be registered for the Kind Agent.
func (d *AgentCustomDefaulter) Default(ctx context.Context, obj runtime.Object) error {
	agent, ok := obj.(*agentsv1alpha1.Agent)

	if !ok {
		return fmt.Errorf("expected an Agent object but got %T", obj)
	}
	agentlog.Info("Webhook processing for Agent", "name", agent.GetName())

	// Ensure PodTemplateSpec exists
	if agent.Spec.PodTemplateSpec == nil {
		agent.Spec.PodTemplateSpec = &corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{},
		}
	}

	// Use shared pod mutator for injection
	return d.Mutator.MutatePodSpec(
		ctx,
		&agent.Spec.PodTemplateSpec.Spec,
		agent.Namespace,
		agent.Name,
		agent.Annotations,
	)
}

// +kubebuilder:webhook:path=/validate-agent-kagenti-dev-v1alpha1-agent,mutating=false,failurePolicy=fail,sideEffects=None,groups=agent.kagenti.dev,resources=agents,verbs=create;update,versions=v1alpha1,name=vagent-v1alpha1.kb.io,admissionReviewVersions=v1

// AgentCustomValidator struct is responsible for validating the Agent resource
// when it is created, updated, or deleted.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as this struct is used only for temporary operations and does not need to be deeply copied.
type AgentCustomValidator struct {
	// TODO(user): Add more fields as needed for validation
}

var _ webhook.CustomValidator = &AgentCustomValidator{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type Agent.
func (v *AgentCustomValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	agent, ok := obj.(*agentsv1alpha1.Agent)
	if !ok {
		return nil, fmt.Errorf("expected an Agent object but got %T", obj)
	}
	agentlog.Info("Validation for Agent upon creation", "name", agent.GetName())

	// TODO(user): fill in your validation logic upon object creation.

	return nil, nil
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type Agent.
func (v *AgentCustomValidator) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	agent, ok := newObj.(*agentsv1alpha1.Agent)
	if !ok {
		return nil, fmt.Errorf("expected an Agent object for the newObj but got %T", newObj)
	}
	agentlog.Info("Validation for Agent upon update", "name", agent.GetName())

	// TODO(user): fill in your validation logic upon object update.

	return nil, nil
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type Agent.
func (v *AgentCustomValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	agent, ok := obj.(*agentsv1alpha1.Agent)
	if !ok {
		return nil, fmt.Errorf("expected an Agent object but got %T", obj)
	}
	agentlog.Info("Validation for Agent upon deletion", "name", agent.GetName())

	// TODO(user): fill in your validation logic upon object deletion.

	return nil, nil
}
