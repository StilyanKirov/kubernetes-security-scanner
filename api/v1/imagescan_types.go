/*
Copyright 2026.

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

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// ImageScanSpec defines the desired state of ImageScan
type ImageScanSpec struct {

	// NamespaceSelector selects Namespaces by their labels.
	// An empty selector matches all Namespaces.
	// +required
	NamespaceSelector metav1.LabelSelector `json:"namespaceSelector"`

	// PodSelector selects Pods within the selected Namespaces.
	// An empty selector matches all Pods in those Namespaces.
	// +required
	PodSelector metav1.LabelSelector `json:"podSelector"`

	// Severity is the minimum vulnerability severity to include.
	// When omitted, all severities are included, including UNKNOWN.
	// +kubebuilder:validation:Pattern=`^(UNKNOWN|LOW|MEDIUM|HIGH|CRITICAL)(,(UNKNOWN|LOW|MEDIUM|HIGH|CRITICAL))*$`
	// +optional
	Severity string `json:"severity,omitempty"`
}

// ScanPhase is the lifecycle phase of a scan.
// +kubebuilder:validation:Enum=Pending;Running;Completed;Failed
type ScanPhase string

const (
	// ScanPhasePending means the scan is waiting to be processed.
	ScanPhasePending ScanPhase = "Pending"

	// ScanPhaseRunning means scanning is in progress.
	ScanPhaseRunning ScanPhase = "Running"

	// ScanPhaseCompleted means scanning and result storage succeeded.
	ScanPhaseCompleted ScanPhase = "Completed"

	// ScanPhaseFailed means scanning or result storage failed.
	ScanPhaseFailed ScanPhase = "Failed"
)

// ImageScanResult tracks the scan of one unique image.
type ImageScanResult struct {
	// ID uniquely identifies this image scan and its stored result.
	// The controller assigns it once and reuses it on subsequent reconciliations.
	// +kubebuilder:validation:MinLength=1
	// +required
	ID string `json:"id"`

	// Image is the container image reference being scanned.
	// +kubebuilder:validation:MinLength=1
	// +required
	Image string `json:"image"`

	// Phase is the lifecycle phase of this image scan.
	// +required
	Phase ScanPhase `json:"phase"`

	// Message describes progress or the reason for failure.
	// +optional
	Message string `json:"message,omitempty"`
}

// ImageScanStatus defines the observed state of ImageScan.
type ImageScanStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// For Kubernetes API conventions, see:
	// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties

	// Phase is the overall lifecycle phase of the request.
	// Completed means all image scans and result storage succeeded.
	// Finding vulnerabilities does not by itself make the request Failed.
	// +optional
	Phase ScanPhase `json:"phase,omitempty"`

	// Scans contains one entry per unique image discovered.
	// Vulnerability reports are stored in persistent storage, not in this field.
	// +listType=map
	// +listMapKey=image
	// +optional
	Scans []ImageScanResult `json:"scans,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
// +kubebuilder:validation:XValidation:rule="self.spec == oldSelf.spec",message="ImageScan spec is immutable"

// ImageScan is the Schema for the imagescans API
type ImageScan struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of ImageScan
	// +required
	Spec ImageScanSpec `json:"spec"`

	// status defines the observed state of ImageScan
	// +optional
	Status ImageScanStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// ImageScanList contains a list of ImageScan
type ImageScanList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []ImageScan `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &ImageScan{}, &ImageScanList{})
		// Регистрира Go типовете ImageScan и ImageScanList
		// за съответната API група и версия в runtime.Scheme.
		return nil
	})
}
