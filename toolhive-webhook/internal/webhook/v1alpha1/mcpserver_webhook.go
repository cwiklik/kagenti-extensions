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

	toolhivestacklokdevv1alpha1 "github.com/stacklok/toolhive/cmd/thv-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// nolint:unused
// log is for logging in this package.
var mcpserverlog = logf.Log.WithName("mcpserver-resource")

const (
	CLIENT_REGISTRATION_NAME = "kagenti-client-registration"
	SPIFFY_HELPER_NAME       = "spiffe-helper"
)

// SetupMCPServerWebhookWithManager registers the webhook for MCPServer in the manager.
func SetupMCPServerWebhookWithManager(mgr ctrl.Manager, registerClient bool) error {

	return ctrl.NewWebhookManagedBy(mgr).
		For(&toolhivestacklokdevv1alpha1.MCPServer{}).
		WithValidator(&MCPServerCustomValidator{}).
		WithDefaulter(&MCPServerCustomDefaulter{registerClient}).
		Complete()
}

// TODO(user): EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

// +kubebuilder:webhook:path=/mutate-toolhive-stacklok-dev-v1alpha1-mcpserver,mutating=true,failurePolicy=fail,sideEffects=None,groups=toolhive.stacklok.dev,resources=mcpservers,verbs=create;update,versions=v1alpha1,name=mmcpserver-v1alpha1.kb.io,admissionReviewVersions=v1

// MCPServerCustomDefaulter struct is responsible for setting default values on the custom resource of the
// Kind MCPServer when those are created or updated.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as it is used only for temporary operations and does not need to be deeply copied.
type MCPServerCustomDefaulter struct {
	EnableClientRegistration bool
}

var _ webhook.CustomDefaulter = &MCPServerCustomDefaulter{}

// Default implements webhook.CustomDefaulter so a webhook will be registered for the Kind MCPServer.
func (d *MCPServerCustomDefaulter) Default(ctx context.Context, obj runtime.Object) error {
	mcpserver, ok := obj.(*toolhivestacklokdevv1alpha1.MCPServer)

	if !ok {
		return fmt.Errorf("expected an MCPServer object but got %T", obj)
	}
	mcpserverlog.Info("Defaulting for MCPServer", "name", mcpserver.GetName())

	if mcpserver.Spec.PodTemplateSpec == nil {
		mcpserver.Spec.PodTemplateSpec = &corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{},
		}
	}
	if d.EnableClientRegistration {
		// Check if the kagenti-client-registration Container already exists
		if exists := d.containerExists(mcpserver, CLIENT_REGISTRATION_NAME); !exists {
			if err := d.injectClientRegistrationContainer(mcpserver); err != nil {
				return fmt.Errorf("failed to inject client-registration container: %w", err)
			}
		}

		// Check if the spiffy-helper Container already exists
		if exists := d.containerExists(mcpserver, SPIFFY_HELPER_NAME); !exists {
			if err := d.injectSpiffyHelperContainer(mcpserver); err != nil {
				return fmt.Errorf("failed to inject spiffy-helper container: %w", err)
			}
		}

		// Check Volumes

		if exists := d.volumeExists(mcpserver, "shared-data"); !exists {
			mcpserver.Spec.PodTemplateSpec.Spec.Volumes = append(mcpserver.Spec.PodTemplateSpec.Spec.Volumes, corev1.Volume{
				Name: "shared-data",
				VolumeSource: corev1.VolumeSource{
					EmptyDir: &corev1.EmptyDirVolumeSource{},
				},
			})
		}

		if exists := d.volumeExists(mcpserver, "spire-agent-socket"); !exists {
			mcpserver.Spec.PodTemplateSpec.Spec.Volumes = append(mcpserver.Spec.PodTemplateSpec.Spec.Volumes, corev1.Volume{
				Name: "spire-agent-socket",
				VolumeSource: corev1.VolumeSource{
					HostPath: &corev1.HostPathVolumeSource{
						Path: "/run/spire/agent-sockets",
					},
				},
			})
		}

		if exists := d.volumeExists(mcpserver, "spiffe-helper-config"); !exists {
			mcpserver.Spec.PodTemplateSpec.Spec.Volumes = append(mcpserver.Spec.PodTemplateSpec.Spec.Volumes, corev1.Volume{
				Name: "spiffe-helper-config",
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "spiffe-helper-config",
						},
					},
				},
			})
		}

		if exists := d.volumeExists(mcpserver, "svid-output"); !exists {
			mcpserver.Spec.PodTemplateSpec.Spec.Volumes = append(mcpserver.Spec.PodTemplateSpec.Spec.Volumes, corev1.Volume{
				Name: "svid-output",
				VolumeSource: corev1.VolumeSource{
					EmptyDir: &corev1.EmptyDirVolumeSource{},
				},
			})
		}
	}
	return nil
}
func (d *MCPServerCustomDefaulter) containerExists(mcpserver *toolhivestacklokdevv1alpha1.MCPServer, containerName string) bool {
	for _, container := range mcpserver.Spec.PodTemplateSpec.Spec.Containers {
		if container.Name == containerName {
			return true
		}
	}

	return false
}
func (d *MCPServerCustomDefaulter) volumeExists(mcpserver *toolhivestacklokdevv1alpha1.MCPServer, volumeName string) bool {

	for _, vol := range mcpserver.Spec.PodTemplateSpec.Spec.Volumes {
		if vol.Name == volumeName {
			return true
		}
	}
	return false
}
func (d *MCPServerCustomDefaulter) injectClientRegistrationContainer(mcpserver *toolhivestacklokdevv1alpha1.MCPServer) error {

	containers := mcpserver.Spec.PodTemplateSpec.Spec.Containers
	if len(containers) == 0 {
		return fmt.Errorf("no containers found in MCPServer spec")
	}

	imagePullPolicy := "IfNotPresent"
	resources := corev1.ResourceRequirements{
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("100m"),
			corev1.ResourceMemory: resource.MustParse("128Mi"),
		},
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("50m"),
			corev1.ResourceMemory: resource.MustParse("64Mi"),
		},
	}
	clientId := mcpserver.Namespace + "/" + mcpserver.Name
	containers = append(containers, corev1.Container{
		Name:            CLIENT_REGISTRATION_NAME,
		Image:           "ghcr.io/kagenti/kagenti/client-registration:latest",
		ImagePullPolicy: corev1.PullPolicy(imagePullPolicy),
		Resources:       resources,
		// Wait until /opt/jwt_svid.token appears, then exec
		Command: []string{
			"/bin/sh",
			"-c",
			// TODO: tail -f /dev/null allows the container to stay alive. Change this to be a job.
			"while [ ! -f /opt/jwt_svid.token ]; do echo waiting for SVID; sleep 1; done; python client_registration.py; tail -f /dev/null",
		},
		Env: []corev1.EnvVar{
			{
				Name: "KEYCLOAK_URL",
				ValueFrom: &corev1.EnvVarSource{
					ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "environments",
						},
						Key:      "KEYCLOAK_URL",
						Optional: ptr.To(true),
					},
				},
			},
			{
				Name: "KEYCLOAK_REALM",
				ValueFrom: &corev1.EnvVarSource{
					ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "environments",
						},
						Key: "KEYCLOAK_REALM",
					},
				},
			},
			{
				Name: "KEYCLOAK_ADMIN_USERNAME",
				ValueFrom: &corev1.EnvVarSource{
					ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "environments",
						},
						Key: "KEYCLOAK_ADMIN_USERNAME",
					},
				},
			},
			{
				Name: "KEYCLOAK_ADMIN_PASSWORD",
				ValueFrom: &corev1.EnvVarSource{
					ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "environments",
						},
						Key: "KEYCLOAK_ADMIN_PASSWORD",
					},
				},
			},
			{
				Name:  "CLIENT_NAME",
				Value: clientId,
			},
			{
				Name:  "CLIENT_ID",
				Value: "spiffe://localtest.me/sa/" + mcpserver.Name,
			},
			{
				Name:  "NAMESPACE",
				Value: mcpserver.Namespace,
			},
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				// This is how client registration accesses the SVID
				Name:      "svid-output",
				MountPath: "/opt",
			},
		},
	})
	mcpserver.Spec.PodTemplateSpec.Spec.Containers = containers
	return nil
}
func (d *MCPServerCustomDefaulter) injectSpiffyHelperContainer(mcpserver *toolhivestacklokdevv1alpha1.MCPServer) error {

	containers := mcpserver.Spec.PodTemplateSpec.Spec.Containers
	if len(containers) == 0 {
		return fmt.Errorf("no containers found in MCPServer spec")
	}

	imagePullPolicy := "IfNotPresent"
	resources := corev1.ResourceRequirements{
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("100m"),
			corev1.ResourceMemory: resource.MustParse("128Mi"),
		},
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("50m"),
			corev1.ResourceMemory: resource.MustParse("64Mi"),
		},
	}

	containers = append(containers, corev1.Container{
		Name:            SPIFFY_HELPER_NAME,
		Image:           "ghcr.io/spiffe/spiffe-helper:nightly",
		ImagePullPolicy: corev1.PullPolicy(imagePullPolicy),
		Resources:       resources,
		// Wait until /opt/jwt_svid.token appears, then exec
		Command: []string{
			"/spiffe-helper",
			"-config=/etc/spiffe-helper/helper.conf",
			"run",
		},

		VolumeMounts: []corev1.VolumeMount{
			{
				// This is how client registration accesses the SVID
				Name:      "spiffe-helper-config",
				MountPath: "/etc/spiffe-helper",
			},
			{
				// This is how client registration accesses the SVID
				Name:      "spire-agent-socket",
				MountPath: "/spiffe-workload-api",
			},
			{
				// This is how client registration accesses the SVID
				Name:      "svid-output",
				MountPath: "/opt",
			},
		},
	})
	mcpserver.Spec.PodTemplateSpec.Spec.Containers = containers
	return nil
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
		return nil, fmt.Errorf("expected a MCPServer object but got %T", obj)
	}
	mcpserverlog.Info("Validation for MCPServer upon deletion", "name", mcpserver.GetName())

	// TODO(user): fill in your validation logic upon object deletion.

	return nil, nil
}
