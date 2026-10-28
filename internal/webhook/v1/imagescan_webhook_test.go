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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	scanv1 "github.com/Stilyan-Kirov/my-operator/api/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("ImageScan Webhook", func() {
	var (
		obj       *scanv1.ImageScan
		oldObj    *scanv1.ImageScan
		validator ImageScanValidator
	)

	BeforeEach(func() {
		obj = &scanv1.ImageScan{}
		oldObj = &scanv1.ImageScan{}
		validator = ImageScanValidator{}
		Expect(validator).NotTo(BeNil(), "Expected validator to be initialized")
		Expect(oldObj).NotTo(BeNil(), "Expected oldObj to be initialized")
		Expect(obj).NotTo(BeNil(), "Expected obj to be initialized")
	})

	AfterEach(func() {
		// TODO (user): Add any teardown logic common to all tests
	})

	Context("When creating or updating ImageScan under Validating Webhook", func() {
		It("accepts a scan when a Pod matches both selectors", func() {
			namespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "imagescan-test",
					Labels: map[string]string{"env": "production"},
				},
			}
			Expect(k8sClient.Create(ctx, namespace)).To(Succeed())
			DeferCleanup(func() {
				Expect(k8sClient.Delete(ctx, namespace)).To(Succeed())
			})

			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "web-test",
					Namespace: namespace.Name,
					Labels:    map[string]string{"app": "web"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "nginx", Image: "nginx:1.27"}},
				},
			}
			Expect(k8sClient.Create(ctx, pod)).To(Succeed())
			DeferCleanup(func() {
				Expect(k8sClient.Delete(ctx, pod)).To(Succeed())
			})

			obj.Spec.NamespaceSelector.MatchLabels = map[string]string{"env": "production"}
			obj.Spec.PodSelector.MatchLabels = map[string]string{"app": "web"}
			validator.Reader = k8sClient

			warnings, err := validator.ValidateCreate(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(BeEmpty())
		})

		// TODO (user): Add logic for validating webhooks
		// Example:
		// It("Should deny creation if a required field is missing", func() {
		//     By("simulating an invalid creation scenario")
		//     obj.SomeRequiredField = ""
		//     Expect(validator.ValidateCreate(ctx, obj)).Error().To(HaveOccurred())
		// })
		//
		// It("Should admit creation if all required fields are present", func() {
		//     By("simulating an invalid creation scenario")
		//     obj.SomeRequiredField = "valid_value"
		//     Expect(validator.ValidateCreate(ctx, obj)).To(BeNil())
		// })
		//
		// It("Should validate updates correctly", func() {
		//     By("simulating a valid update scenario")
		//     oldObj.SomeRequiredField = "updated_value"
		//     obj.SomeRequiredField = "updated_value"
		//     Expect(validator.ValidateUpdate(ctx, oldObj, obj)).To(BeNil())
		// })
	})

})
