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

package v1beta1

import (
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// OAUTH2ProxySpec defines the desired state of OAUTH2Proxy
type OAUTH2ProxySpec struct {
	// +required
	Host string `json:"host"`

	// +required
	RedirectURI string `json:"redirectURI"`

	// +optional
	WhitelistPaths []string `json:"whitelistPath,omitempty"`

	// +required
	Backend ServiceSelector `json:"backend"`
}

type ServiceSelector struct {
	ServiceName string `json:"serviceName"`
	ServicePort string `json:"servicePort"`
}

// OAUTH2ProxyStatus defines the observed state of OAUTH2Proxy
type OAUTH2ProxyStatus struct {
	// Conditions holds the conditions for the VaultBinding.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

const (
	ReadyCondition            = "Ready"
	ServicePortNotFoundReason = "ServicePortNotFound"
	ServiceNotFoundReason     = "ServiceNotFound"
	ServiceBackendReadyReason = "ServiceBackendReady"
)

// ConditionalResource is a resource with conditions
type conditionalResource interface {
	GetStatusConditions() *[]metav1.Condition
}

// setResourceCondition sets the given condition with the given status,
// reason and message on a resource.
func setResourceCondition(resource conditionalResource, condition string, status metav1.ConditionStatus, reason, message string) {
	conditions := resource.GetStatusConditions()

	newCondition := metav1.Condition{
		Type:    condition,
		Status:  status,
		Reason:  reason,
		Message: message,
	}

	apimeta.SetStatusCondition(conditions, newCondition)
}

// OAUTH2ProxyNotReady
func OAUTH2ProxyNotReady(clone OAUTH2Proxy, reason, message string) OAUTH2Proxy {
	setResourceCondition(&clone, ReadyCondition, metav1.ConditionFalse, reason, message)
	return clone
}

// OAUTH2ProxyReady
func OAUTH2ProxyReady(clone OAUTH2Proxy, reason, message string) OAUTH2Proxy {
	setResourceCondition(&clone, ReadyCondition, metav1.ConditionTrue, reason, message)
	return clone
}

// GetStatusConditions returns a pointer to the Status.Conditions slice
func (in *OAUTH2Proxy) GetStatusConditions() *[]metav1.Condition {
	return &in.Status.Conditions
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:shortName=rc
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type==\"Ready\")].status",description=""
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.conditions[?(@.type==\"Ready\")].message",description=""
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description=""

// OAUTH2Proxy is the Schema for the OAUTH2Proxys API
type OAUTH2Proxy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   OAUTH2ProxySpec   `json:"spec,omitempty"`
	Status OAUTH2ProxyStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// OAUTH2ProxyList contains a list of OAUTH2Proxy
type OAUTH2ProxyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []OAUTH2Proxy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&OAUTH2Proxy{}, &OAUTH2ProxyList{})
}
