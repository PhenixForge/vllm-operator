package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// VLLMModelSpec defines the desired state of VLLMModel
type VLLMModelSpec struct {
	ModelId      string            `json:"modelId"`
	Replicas     int32             `json:"replicas"`
	StorageSize  string            `json:"storageSize"`
	StorageClass string            `json:"storageClass"`
	Resources    ResourceRequirements `json:"resources,omitempty"`
	Env          []EnvVar          `json:"env,omitempty"`
	HPA          *HPAConfig        `json:"hpa,omitempty"`
}

type ResourceRequirements struct {
	Limits   ResourceList `json:"limits,omitempty"`
	Requests ResourceList `json:"requests,omitempty"`
}

type ResourceList map[string]string

type EnvVar struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type HPAConfig struct {
	Enabled                bool `json:"enabled"`
	MinReplicas            int32 `json:"minReplicas"`
	MaxReplicas            int32 `json:"maxReplicas"`
	TargetCPUUtilization   int32 `json:"targetCPUUtilization,omitempty"`
	TargetMemoryUtilization int32 `json:"targetMemoryUtilization,omitempty"`
}

// VLLMModelStatus defines the observed state of VLLMModel
type VLLMModelStatus struct {
	Phase           string      `json:"phase,omitempty"`
	PodsReady       int32       `json:"podsReady"`
	PVCName         string      `json:"pvcName,omitempty"`
	JobName         string      `json:"jobName,omitempty"`
	DeploymentName  string      `json:"deploymentName,omitempty"`
	LastUpdate      metav1.Time `json:"lastUpdate,omitempty"`
	Message         string      `json:"message,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

type VLLMModel struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec   VLLMModelSpec   `json:"spec,omitempty"`
	Status VLLMModelStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

type VLLMModelList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VLLMModel `json:"items"`
}

func init() {
	SchemeBuilder.Register(&VLLMModel{}, &VLLMModelList{})
}