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
	"context"
	"fmt"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	scanv1 "github.com/Stilyan-Kirov/my-operator/api/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// nolint:unused
// log is for logging in this package.
var imagescanlog = logf.Log.WithName("imagescan-resource")

// SetupImageScanWebhookWithManager registers the webhook for ImageScan in the manager.
func SetupImageScanWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &scanv1.ImageScan{}).
		WithValidator(&ImageScanValidator{Reader: mgr.GetAPIReader()}).
		Complete()
}

// mgr.GetAPIReader() подава четец, който пита API сървъра директно.
// Така проверката при създаване не разчита на евентуално остарял кеш.

// TODO(user): EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// NOTE: If you want to customise the 'path', use the flags '--defaulting-path' or '--validation-path'.
// +kubebuilder:webhook:path=/validate-scan-my-domain-v1-imagescan,mutating=false,failurePolicy=fail,sideEffects=None,groups=scan.my.domain,resources=imagescans,verbs=create;update,versions=v1,name=vimagescan-v1.kb.io,admissionReviewVersions=v1

// ImageScanValidator struct is responsible for validating the ImageScan resource
// when it is created, updated, or deleted.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as this struct is used only for temporary operations and does not need to be deeply copied.
type ImageScanValidator struct {
	// Reader is used to read Namespace and Pod resources during ValidateCreate.
	Reader client.Reader
}

// ValidateCreate implements admission.Validator so a webhook will be registered for the type ImageScan.
func (v *ImageScanValidator) ValidateCreate(ctx context.Context, obj *scanv1.ImageScan) (admission.Warnings, error) {
	imagescanlog.Info("Validation for ImageScan upon creation", "name", obj.GetName())

	if v.Reader == nil {
		return nil, fmt.Errorf("ImageScan validator has no API reader")
	}

	namespaceSelector, err := metav1.LabelSelectorAsSelector(&obj.Spec.NamespaceSelector) // проверка дали spec.namespaceSelector е валиден LabelSelector
	if err != nil {
		return nil, fmt.Errorf("invalid spec.namespaceSelector: %w", err)
	}

	podSelector, err := metav1.LabelSelectorAsSelector(&obj.Spec.PodSelector) // проверка дали spec.podSelector е валиден LabelSelector
	if err != nil {
		return nil, fmt.Errorf("invalid spec.podSelector: %w", err)
	}

	namespaces := &corev1.NamespaceList{}
	namespaceFilter := client.MatchingLabelsSelector{Selector: namespaceSelector}
	err = v.Reader.List(ctx, namespaces, namespaceFilter) // създава се списък от Namespace-и, които съвпадат с namespaceSelector
	if err != nil {
		return nil, fmt.Errorf("could not list matching Namespaces: %w", err)
	}

	for _, namespace := range namespaces.Items {
		// var pods corev1.PodList
		pods := &corev1.PodList{}
		podFilter := client.MatchingLabelsSelector{Selector: podSelector}
		innamespace := client.InNamespace(namespace.Name)
		err = v.Reader.List(ctx, pods, innamespace, podFilter)
		/* Този ред иска от Kubernetes: „Дай ми Pods в namespace.Name,
		чиито етикети съвпадат с podSelector.“ Резултатите се записват в pods.Items. */
		if err != nil {
			return nil, fmt.Errorf("could not list Pods in Namespace %q: %w", namespace.Name, err)
		}

		// at least one Pod matches the namespaceSelector and podSelector, validation is successful
		if len(pods.Items) > 0 {
			return nil, nil
		}
	}

	// No pod matches the spec.namespaceSelector and spec.podSelector, validation fails
	return nil, fmt.Errorf(
		"no Pod matches spec.namespaceSelector=%q and spec.podSelector=%q",
		namespaceSelector.String(), podSelector.String(),
	)
}

// ValidateUpdate implements admission.Validator so a webhook will be registered for the type ImageScan.
func (v *ImageScanValidator) ValidateUpdate(_ context.Context, oldObj, newObj *scanv1.ImageScan) (admission.Warnings, error) {
	imagescanlog.Info("Validation for ImageScan upon update", "name", newObj.GetName())

	// The CRD enforces spec immutability. Pod existence is checked only on creation.

	return nil, nil
}

// ValidateDelete implements admission.Validator so a webhook will be registered for the type ImageScan.
func (v *ImageScanValidator) ValidateDelete(_ context.Context, obj *scanv1.ImageScan) (admission.Warnings, error) {
	imagescanlog.Info("Validation for ImageScan upon deletion", "name", obj.GetName())

	// Deletion does not require a matching Pod.

	return nil, nil
}
