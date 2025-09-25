package v2alpha2

import (
	"fmt"
	"strings"

	jsoniter "github.com/json-iterator/go"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
)

func init() {
	SchemeBuilder.Register(&Gateway{}, &GatewayList{})
	SchemeBuilder.Register(&UpgradePlan{}, &UpgradePlanList{})
}

const (
	UpgradePlanStatePending           = "Pending"
	UpgradePlanStateRunning           = "Running"
	UpgradePlanStateSucceeded         = "Succeeded"
	UpgradePlanStateFailed            = "Failed"
	UpgradePlanStateEmpty             = ""
	KubeSphereControlsSystemNamespace = "kubesphere-controls-system"

	GatewayKind                  = "Gateway"
	GatewayResource              = "gateways"
	ConditionTypeDeployd         = "Deployed"
	ConditionTypeDeploymentReady = "DeploymentReady"
	StatusTrue                   = "True"

	LabelUpgradePlanName = "gateway.kubesphere.io/upgradeplan-name"
)

type ReplicaRange struct {
	// +optional
	Min *int32 `json:"min"`
	// +optional
	Max *int32 `json:"max"`
}

// GatewaySpec defines the desired state of Gateway
type GatewaySpec struct {
	AppVersion string `json:"appVersion"`

	// +optional
	ReplicaRange ReplicaRange `json:"replicaRange,omitempty"`
	// +kubebuilder:pruning:PreserveUnknownFields
	Values runtime.RawExtension `json:"values"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:storageversion

// Gateway is the Schema for the gateways API
type Gateway struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              GatewaySpec `json:"spec,omitempty"`
	// +kubebuilder:pruning:PreserveUnknownFields
	Status runtime.RawExtension `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type Status struct {
	LoadBalancer corev1.LoadBalancerStatus `json:"loadBalancer"`
	Service      []corev1.ServicePort      `json:"service"`
	Conditions   []metav1.Condition        `json:"conditions"`
}

func (src *Status) DeepCopy() *Status {
	if src == nil {
		return nil
	}
	copyServices := make([]corev1.ServicePort, len(src.Service))
	for i, svc := range src.Service {
		copyServices[i] = *(svc.DeepCopy())
	}
	copyConditions := make([]metav1.Condition, len(src.Conditions))
	for i, cond := range src.Conditions {
		copyConditions[i] = *(cond.DeepCopy())
	}
	copyLoadBalancer := *(src.LoadBalancer.DeepCopy())
	return &Status{
		LoadBalancer: copyLoadBalancer,
		Service:      copyServices,
		Conditions:   copyConditions,
	}
}

func (in *Gateway) GetStatus() (*Status, error) {
	status := &Status{}
	if in.Status.Raw != nil {
		err := jsoniter.Unmarshal(in.Status.Raw, status)
		if err != nil {
			return nil, err
		}
	}
	return status, nil
}

func (in *Gateway) SetStatus(status *Status) error {
	statusRaw, err := jsoniter.Marshal(status)
	if err != nil {
		return err
	}
	in.Status.Raw = statusRaw
	return nil
}

func (in *Gateway) IsDeployed() bool {
	status, err := in.GetStatus()
	if err != nil {
		return false
	}
	for _, condition := range status.Conditions {
		if condition.Type == ConditionTypeDeployd && condition.Status == StatusTrue {
			return true
		}
	}
	return false
}

func (in *Gateway) IsDeploymentReady() bool {
	status, err := in.GetStatus()
	if err != nil {
		return false
	}
	for _, condition := range status.Conditions {
		if condition.Type == ConditionTypeDeploymentReady && condition.Status == StatusTrue {
			return true
		}
	}
	return false
}

//+kubebuilder:object:root=true

// GatewayList contains a list of Gateway
type GatewayList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Gateway `json:"items"`
}

type GatewayReference struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
}

func (in *GatewayReference) ToString() string {
	namespace := in.Namespace
	if namespace == "" {
		namespace = KubeSphereControlsSystemNamespace
	}
	return fmt.Sprintf("%s/%s", namespace, in.Name)
}

func (in *GatewayReference) FromString(name string) {
	split := strings.Split(name, "/")
	lenSplit := len(split)
	if lenSplit == 0 {
		return
	} else if lenSplit > 0 && lenSplit < 2 {
		in.Name = split[0]
		in.Namespace = KubeSphereControlsSystemNamespace
	} else {
		in.Name = split[0]
		in.Namespace = split[1]
	}
}

func (in *GatewayReference) ToNamespacedName() types.NamespacedName {
	namespace := in.Namespace
	if namespace == "" {
		namespace = KubeSphereControlsSystemNamespace
	}
	return types.NamespacedName{Namespace: namespace, Name: in.Name}
}

type UpgradePlanSpec struct {
	GatewayRefs      []GatewayReference   `json:"gatewayReferences,omitempty"`
	TargetAPPVersion string               `json:"targetAppVersion"`
	Values           runtime.RawExtension `json:"values,omitempty"`
}

type UpgradePlanStatus struct {
	State      string      `json:"state,omitempty"`
	Version    string      `json:"version,omitempty"`
	JobName    string      `json:"jobName,omitempty"`
	Message    string      `json:"message,omitempty"`
	LastUpdate metav1.Time `json:"lastUpdate,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:printcolumn:name="State",type="string",JSONPath=".status.state"
type UpgradePlan struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              UpgradePlanSpec   `json:"spec,omitempty"`
	Status            UpgradePlanStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
// GatewayList contains a list of Gateway
type UpgradePlanList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []UpgradePlan `json:"items"`
}
