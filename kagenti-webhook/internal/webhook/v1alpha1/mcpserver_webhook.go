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
	toolhivestacklokdevv1alpha1 "github.com/stacklok/toolhive/cmd/thv-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// nolint:unused
// log is for logging in this package.
var mcpserverlog = logf.Log.WithName("mcpserver-resource")

// SetupMCPServerWebhookWithManager registers the webhook for MCPServer in the manager.
func SetupMCPServerWebhookWithManager(mgr ctrl.Manager, mutator *injector.PodMutator) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(&toolhivestacklokdevv1alpha1.MCPServer{}).
		WithValidator(&MCPServerCustomValidator{}).
		WithDefaulter(&MCPServerCustomDefaulter{Mutator: mutator}).
		Complete()
}

// +kubebuilder:webhook:path=/mutate-toolhive-stacklok-dev-v1alpha1-mcpserver,mutating=true,failurePolicy=fail,sideEffects=None,groups=toolhive.stacklok.dev,resources=mcpservers,verbs=create;update,versions=v1alpha1,name=mmcpserver-v1alpha1.kb.io,admissionReviewVersions=v1

// MCPServerCustomDefaulter struct is responsible for setting default values on the custom resource of the
// Kind MCPServer when those are created or updated.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as it is used only for temporary operations and does not need to be deeply copied.
type MCPServerCustomDefaulter struct {
	Mutator *injector.PodMutator
}

var _ webhook.CustomDefaulter = &MCPServerCustomDefaulter{}

// Default implements webhook.CustomDefaulter so a webhook will be registered for the Kind MCPServer.
func (d *MCPServerCustomDefaulter) Default(ctx context.Context, obj runtime.Object) error {
	mcpserver, ok := obj.(*toolhivestacklokdevv1alpha1.MCPServer)

	if !ok {
		return fmt.Errorf("expected an MCPServer object but got %T", obj)
	}
	mcpserverlog.Info("Defaulting for MCPServer", "name", mcpserver.GetName())

	// Ensure PodTemplateSpec exists
	if mcpserver.Spec.PodTemplateSpec == nil {
		mcpserver.Spec.PodTemplateSpec = &corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{},
		}
	}

	// Use shared pod mutator for injection
	return d.Mutator.MutatePodSpec(
		ctx,
		&mcpserver.Spec.PodTemplateSpec.Spec,
		mcpserver.Namespace,
		mcpserver.Name,
		mcpserver.Annotations,
	)
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// NOTE: The 'path' attribute must follow a specific pattern and should not be modified directly here.
// Modifying the path for an invalid path can cause API server errors; failing to locate the webhook.
// +kubebuilder:webhook:path=/validate-toolhive-stacklok-dev-v1alpha1-mcpserver,mutating=false,failurePolicy=fail,sideEffects=None,groups=toolhive.stacklok.dev,resources=mcpservers,verbs=create;update,versions=v1alpha1,name=vmcpserver-v1alpha1.kb.io,admissionReviewVersions=v1

// MCPServerCustomValidator struct is responsible for validating the MCPServer resource
// when it is created, updated, or deleted.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as this struct is used only for temporary operations and does not need to be deeply copied.
type MCPServerCustomValidator struct {
	// TODO(user): Add more fields as needed for validation
}

var _ webhook.CustomValidator = &MCPServerCustomValidator{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type MCPServer.
func (v *MCPServerCustomValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	mcpserver, ok := obj.(*toolhivestacklokdevv1alpha1.MCPServer)
	if !ok {
		return nil, fmt.Errorf("expected a MCPServer object but got %T", obj)
	}
	mcpserverlog.Info("Validation for MCPServer upon creation", "name", mcpserver.GetName())

	// TODO(user): fill in your validation logic upon object creation.

	return nil, nil
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type MCPServer.
func (v *MCPServerCustomValidator) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	mcpserver, ok := newObj.(*toolhivestacklokdevv1alpha1.MCPServer)
	if !ok {
		return nil, fmt.Errorf("expected a MCPServer object for the newObj but got %T", newObj)
	}
	mcpserverlog.Info("Validation for MCPServer upon update", "name", mcpserver.GetName())

	// TODO(user): fill in your validation logic upon object update.

	return nil, nil
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type MCPServer.
func (v *MCPServerCustomValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	mcpserver, ok := obj.(*toolhivestacklokdevv1alpha1.MCPServer)
	if !ok {
		return nil, fmt.Errorf("expected an MCPServer object but got %T", obj)
	}
	mcpserverlog.Info("Validation for MCPServer upon deletion", "name", mcpserver.GetName())

	// TODO(user): fill in your validation logic upon object deletion.

	return nil, nil
}
