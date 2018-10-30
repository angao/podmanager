/*

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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// IPSet is a set of ip
type IPSet []string

// PodManagerSpec defines the desired state of PodManager
type PodManagerSpec struct {
	// The deployment need to gray release or ref pod need to delete
	DeploymentName string `json:"deploymentName,omitempty" protobuf:"bytes,1,opt,name=deploymentName"`

	// Specify the execute time of grayscale upgrade, if it's not set, means that executed immediately
	// +optional
	ScaleTimestamp string `json:"scaleTimestamp,omitempty" protobuf:"bytes,2,opt,name=scaleTimestamp"`

	// Specify these IPs for pod need to delete or upgrade
	IPSet IPSet `json:"ipSet,omitempty" protobuf:"bytes,3,opt,name=ipSet"`

	// Specify the resources of container which need to upgrade
	Resources *UpdatedContainerResources `json:"resources,omitempty" protobuf:"bytes,4,opt,name=resources"`

	// The PodManager strategy to express pod need to delete or upgrade
	// +optional
	Strategy PodManagerStrategy `json:"strategy,omitempty" protobuf:"bytes,5,opt,name=strategy"`
}

// PodManagerStatus defines the observed state of PodManager
type PodManagerStatus struct {
	// DeploymentUpdated describes the binding deployment whether updated.
	DeploymentUpdated bool `json:"updated,omitempty" protobuf:"bytes,1,opt,name=updated"`
	// Conditions is the set of conditions required for this podmanager to scale its target,
	// and indicates whether or not those conditions are met.
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	Conditions []PodManagerCondition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,2,rep,name=conditions"`
}

// PodManagerCondition describes the state of a PodManager at a certain point.
type PodManagerCondition struct {
	// The last time this condition was updated.
	LastUpdateTime metav1.Time `json:"lastUpdateTime,omitempty" protobuf:"bytes,1,opt,name=lastUpdateTime"`
	// reason is the reason for the condition's last update.
	// +optional
	Reason string `json:"reason,omitempty" protobuf:"bytes,2,opt,name=reason"`
	// message is a human-readable explanation containing details about
	// the transition
	// +optional
	Message string `json:"message,omitempty" protobuf:"bytes,3,opt,name=message"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// PodManager is the Schema for the podmanagers API
// +k8s:openapi-gen=true
type PodManager struct {
	metav1.TypeMeta `json:",inline"`
	// Standard object metadata. More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	// Specification of the behavior of the podmanager.
	// More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#spec-and-status.
	Spec PodManagerSpec `json:"spec" protobuf:"bytes,2,name=spec"`

	// Current information about the podmanager.
	// +optional
	Status PodManagerStatus `json:"status,omitempty" protobuf:"bytes,3,opt,name=status"`
}

// PodManagerStrategyStrategy describes pod delete or upgrade.
type PodManagerStrategy struct {
	// Type of PodManager. Can be "PodUpgrade" or "PodDelete". Default is PodUpgrade.
	// +optional
	Type PodManagerStrategyType `json:"type,omitempty" protobuf:"bytes,1,opt,name=type"`

	// PodActionPhase if type is PodDelete, then it should assign value
	Phase PodActionPhase `json:"phase,omitempty" protobuf:"bytes,2,opt,name=phase"`
}

type PodManagerStrategyType string

const (
	// PodUpgradeStrategyType specify deployment grayscale upgrade.
	PodUpgradeStrategyType PodManagerStrategyType = "PodUpgrade"

	// PodDeleteStrategyType means the Pod IP in IPSet need to delete.
	PodDeleteStrategyType PodManagerStrategyType = "PodDelete"
)

type PodActionPhase string

// controller watch the type, if the type is "Bind", controller will tag the pod with
// annotation(app.sncloud.com/scaledown: true), but pod don't be delete, only if the type change to be "Delete",
// controller will adjust the deployment replicas and replicaset controller will find these pods and delete.
const (
	// Bind is default, means the pod need be deleted later.
	Binding PodActionPhase = "Binding"

	// Delete means the pod will be deleted now.
	Deleting PodActionPhase = "Deleting"
)

// UpdatedContainerResources is a set of ContainerResource.
type UpdatedContainerResources struct {
	Containers []ContainerResource `json:"containers,omitempty" protobuf:"bytes,1,opt,name=containers"`
}

// ContainerResource specify the resource and image of container which need to update.
type ContainerResource struct {
	// Name of the container.
	ContainerName string `json:"containerName,omitempty" protobuf:"bytes,1,opt,name=containerName"`
	// Docker image name.
	// More info: https://kubernetes.io/docs/concepts/containers/images
	// This field is optional to allow higher level config management to default or override
	// container images in workload controllers like Deployments and StatefulSets.
	// +optionals
	Image string `json:"image,omitempty" protobuf:"bytes,2,opt,name=image"`
	// Compute Resources required by this container.
	// Cannot be updated.
	// More info: https://kubernetes.io/docs/concepts/storage/persistent-volumes#resources
	// +optional
	Resources *corev1.ResourceRequirements `json:"resources,omitempty" protobuf:"bytes,3,opt,name=resources"`

	// EnvVar represents an environment variable present in a Container.
	Env []corev1.EnvVar `json:"env,omitempty" protobuf:"bytes,3,opt,name=env"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// PodManagerList contains a list of PodManager
type PodManagerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PodManager `json:"items"`
}
