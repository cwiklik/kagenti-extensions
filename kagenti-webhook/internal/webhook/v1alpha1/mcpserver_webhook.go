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
	"encoding/json"
	"fmt"

	"github.com/kagenti/kagenti-extensions/kagenti-webhook/internal/webhook/injector"
	mcpv1alpha1 "github.com/kagenti/mcp-gateway/pkg/apis/mcp/v1alpha1"
	toolhivestacklokdevv1alpha1 "github.com/stacklok/toolhive/cmd/thv-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// nolint:unused
// log is for logging in this package.
var mcpserverlog = logf.Log.WithName("mcpserver-resource")

const (
	// ManagedByLabel indicates the resource is managed by the webhook
	ManagedByLabel = "toolhive.kagenti.com/managed"
	// AutoGatewayAnnotation controls whether to auto-generate gateway CRs
	AutoGatewayAnnotation = "toolhive.kagenti.com/auto-gateway"
	// MCPGatewayNamespace is the namespace where the MCP gateway resides
	MCPGatewayNamespace = "gateway-system"
	// MCPGatewayName is the name of the MCP gateway
	MCPGatewayName = "mcp-gateway"
)

// SetupMCPServerWebhookWithManager registers the webhook for MCPServer in the manager.
func SetupMCPServerWebhookWithManager(mgr ctrl.Manager, mutator *injector.PodMutator) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(&toolhivestacklokdevv1alpha1.MCPServer{}).
		WithValidator(&MCPServerCustomValidator{}).
		WithDefaulter(&MCPServerCustomDefaulter{
			Mutator: mutator,
			Client:  mgr.GetClient(),
		}).
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
	Client  client.Client
	Decoder *admission.Decoder
}

var _ webhook.CustomDefaulter = &MCPServerCustomDefaulter{}

// Default implements webhook.CustomDefaulter so a webhook will be registered for the Kind MCPServer.
func (d *MCPServerCustomDefaulter) Default(ctx context.Context, obj runtime.Object) error {
	mcpserver, ok := obj.(*toolhivestacklokdevv1alpha1.MCPServer)

	if !ok {
		return fmt.Errorf("expected an MCPServer object but got %T", obj)
	}
	mcpserverlog.Info("Defaulting for MCPServer", "name", mcpserver.GetName())

	// Decode PodTemplateSpec from RawExtension
	var podTemplate *corev1.PodTemplateSpec
	if mcpserver.Spec.PodTemplateSpec != nil && mcpserver.Spec.PodTemplateSpec.Raw != nil {
		podTemplate = &corev1.PodTemplateSpec{}
		if err := json.Unmarshal(mcpserver.Spec.PodTemplateSpec.Raw, podTemplate); err != nil {
			return fmt.Errorf("failed to unmarshal PodTemplateSpec: %w", err)
		}
	} else {
		// Create default PodTemplateSpec if it doesn't exist
		podTemplate = &corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{},
		}
	}

	// Use shared pod mutator for injection
	if err := d.Mutator.MutatePodSpec(
		ctx,
		&podTemplate.Spec,
		mcpserver.Namespace,
		mcpserver.Name,
		mcpserver.Annotations,
	); err != nil {
		return err
	}

	// Marshal the modified PodTemplateSpec back to RawExtension
	modifiedPodTemplateBytes, err := json.Marshal(podTemplate)
	if err != nil {
		return fmt.Errorf("failed to marshal PodTemplateSpec: %w", err)
	}
	mcpserver.Spec.PodTemplateSpec = &runtime.RawExtension{
		Raw: modifiedPodTemplateBytes,
	}

	// Generate MCP Gateway CRs if auto-gateway is enabled
	if d.generateGatewayCRs(mcpserver) {
		if err := d.createGatewayCRs(ctx, mcpserver); err != nil {
			mcpserverlog.Error(err, "Failed to create gateway CRs", "name", mcpserver.GetName())
			return err
		}

	}
	return nil
}

// generateGatewayCRs determines if gateway CRs should be auto-generated
func (d *MCPServerCustomDefaulter) generateGatewayCRs(mcpserver *toolhivestacklokdevv1alpha1.MCPServer) bool {
	// Check for opt-out annotation
	if val, exists := mcpserver.Annotations[AutoGatewayAnnotation]; exists && val == "false" {
		return false
	}
	return true
}

// createGatewayCRs creates or updates the HTTPRoute and MCP Gateway MCPServer CRs
func (d *MCPServerCustomDefaulter) createGatewayCRs(ctx context.Context, mcpserver *toolhivestacklokdevv1alpha1.MCPServer) error {
	// Create HTTPRoute
	httpRoute := d.buildHTTPRoute(mcpserver)
	if err := d.createOrUpdateResource(ctx, httpRoute); err != nil {
		return fmt.Errorf("failed to create/update HTTPRoute: %w", err)
	}

	// Create MCP Gateway MCPServer
	gatewayMCPServer := d.buildGatewayMCPServer(mcpserver)
	if err := d.createOrUpdateResource(ctx, gatewayMCPServer); err != nil {
		return fmt.Errorf("failed to create/update Gateway MCPServer: %w", err)
	}

	mcpserverlog.Info("Successfully created gateway CRs",
		"toolhive-server", mcpserver.GetName(),
		"namespace", mcpserver.Namespace)

	return nil
}

// buildHTTPRoute constructs the HTTPRoute CR for the ToolHive MCPServer
func (d *MCPServerCustomDefaulter) buildHTTPRoute(mcpserver *toolhivestacklokdevv1alpha1.MCPServer) *gatewayv1.HTTPRoute {
	routeName := fmt.Sprintf("%s-route", mcpserver.Name)
	hostname := gatewayv1.Hostname(fmt.Sprintf("%s.mcp.local", mcpserver.Name))
	pathPrefix := gatewayv1.PathMatchPathPrefix
	pathValue := "/"
	gatewayNamespace := gatewayv1.Namespace(MCPGatewayNamespace)

	// Extract service info from MCPServer spec
	serviceName := fmt.Sprintf("mcp-%s-proxy", mcpserver.Name)
	servicePort := gatewayv1.PortNumber(8000) // Default port

	if mcpserver.Spec.PodTemplateSpec != nil {
		if mcpserver.Spec.ProxyPort != 0 {
			servicePort = gatewayv1.PortNumber(mcpserver.Spec.ProxyPort)
		}
	}

	return &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      routeName,
			Namespace: mcpserver.Namespace,
			Labels: map[string]string{
				"mcp-server":   "true",
				ManagedByLabel: "true",
			},
		},
		Spec: gatewayv1.HTTPRouteSpec{
			CommonRouteSpec: gatewayv1.CommonRouteSpec{
				ParentRefs: []gatewayv1.ParentReference{
					{
						Name:      MCPGatewayName,
						Namespace: &gatewayNamespace,
					},
				},
			},
			Hostnames: []gatewayv1.Hostname{hostname},
			Rules: []gatewayv1.HTTPRouteRule{
				{
					Matches: []gatewayv1.HTTPRouteMatch{
						{
							Path: &gatewayv1.HTTPPathMatch{
								Type:  &pathPrefix,
								Value: &pathValue,
							},
						},
					},
					BackendRefs: []gatewayv1.HTTPBackendRef{
						{
							BackendRef: gatewayv1.BackendRef{
								BackendObjectReference: gatewayv1.BackendObjectReference{
									Name: gatewayv1.ObjectName(serviceName),
									Port: &servicePort,
								},
							},
						},
					},
				},
			},
		},
	}
}

// buildGatewayMCPServer constructs the MCP Gateway MCPServer CR
func (d *MCPServerCustomDefaulter) buildGatewayMCPServer(mcpserver *toolhivestacklokdevv1alpha1.MCPServer) *mcpv1alpha1.MCPServer {
	gatewayServerName := fmt.Sprintf("%s-gateway", mcpserver.Name)
	routeName := fmt.Sprintf("%s-route", mcpserver.Name)

	// Extract tool prefix from annotations or use default
	toolPrefix := fmt.Sprintf("%s_", mcpserver.Name)
	if prefix, exists := mcpserver.Annotations["toolhive.kagenti.com/tool-prefix"]; exists {
		toolPrefix = prefix
	}

	return &mcpv1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gatewayServerName,
			Namespace: mcpserver.Namespace,
			Labels: map[string]string{
				ManagedByLabel: "true",
			},
		},
		Spec: mcpv1alpha1.MCPServerSpec{
			ToolPrefix: toolPrefix,
			TargetRef: mcpv1alpha1.TargetReference{
				Group:     "gateway.networking.k8s.io",
				Kind:      "HTTPRoute",
				Name:      routeName,
				Namespace: mcpserver.Namespace,
			},
		},
	}
}

// createOrUpdateResource creates or updates the given Kubernetes resource
func (d *MCPServerCustomDefaulter) createOrUpdateResource(ctx context.Context, obj client.Object) error {
	if d.Client == nil {
		return fmt.Errorf("client is nil")
	}

	key := types.NamespacedName{
		Name:      obj.GetName(),
		Namespace: obj.GetNamespace(),
	}

	// Create a new empty object of the same type for Get operation
	gvk := obj.GetObjectKind().GroupVersionKind()
	if gvk.Empty() {
		// If GVK is not set, try to infer it from the scheme
		gvks, _, err := d.Client.Scheme().ObjectKinds(obj)
		if err != nil || len(gvks) == 0 {
			return fmt.Errorf("failed to get GVK for object: %w", err)
		}
		gvk = gvks[0]
		obj.GetObjectKind().SetGroupVersionKind(gvk)
	}

	// Create a new instance of the same type
	existing, err := d.Client.Scheme().New(gvk)
	if err != nil {
		return fmt.Errorf("failed to create new instance of %s: %w", gvk.String(), err)
	}
	existingObj, ok := existing.(client.Object)
	if !ok {
		return fmt.Errorf("created object is not a client.Object")
	}

	// Try to get existing resource
	err = d.Client.Get(ctx, key, existingObj)

	if errors.IsNotFound(err) {
		// Create new resource
		mcpserverlog.Info("Creating resource",
			"kind", gvk.Kind,
			"name", obj.GetName(),
			"namespace", obj.GetNamespace())
		return d.Client.Create(ctx, obj)
	} else if err != nil {
		return fmt.Errorf("failed to get existing resource: %w", err)
	}

	// Check if resource is managed by us
	if existingObj.GetLabels()[ManagedByLabel] != "true" {
		mcpserverlog.Info("Skipping update of non-managed resource",
			"kind", gvk.Kind,
			"name", obj.GetName())
		return nil
	}

	// Update existing resource
	mcpserverlog.Info("Updating resource",
		"kind", gvk.Kind,
		"name", obj.GetName(),
		"namespace", obj.GetNamespace())
	obj.SetResourceVersion(existingObj.GetResourceVersion())
	return d.Client.Update(ctx, obj)
}

// pointer returns a pointer to the given value
func pointer[T any](v T) *T {
	return &v
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
	Decoder *admission.Decoder
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
