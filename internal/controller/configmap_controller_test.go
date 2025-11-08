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

package controller

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("ConfigMap Controller", func() {

	const (
		TestNamespaceBase = "kubetemplater-test-"
		DriverCMName      = "driver-configmap"
		TargetCMName      = "target-configmap"
		Timeout           = time.Second * 20 // Increased timeout for slower CI environments
		Interval          = time.Millisecond * 250
	)

	var (
		testNs        *corev1.Namespace
		namespaceName string
	)

	BeforeEach(func() {
		// Generate a unique namespace name for each test run
		namespaceName = fmt.Sprintf("%s%d", TestNamespaceBase, time.Now().UnixNano())

		// Create a namespace for the test
		testNs = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespaceName,
			},
		}
		Expect(k8sClient.Create(context.Background(), testNs)).Should(Succeed())
	})

	AfterEach(func() {
		// Delete the namespace and wait for it to be gone
		deletePolicy := metav1.DeletePropagationForeground
		Expect(k8sClient.Delete(context.Background(), testNs, &client.DeleteOptions{
			PropagationPolicy: &deletePolicy,
		})).Should(Succeed())
		Eventually(func() bool {
			err := k8sClient.Get(context.Background(), types.NamespacedName{Name: namespaceName}, &corev1.Namespace{})
			return errors.IsNotFound(err)
		}, Timeout, Interval).Should(BeTrue())
	})

	Context("When a templated ConfigMap is created and then deleted from the driver", func() {
		It("Should create the target resource and then delete it", func() {
			By("Creating a driver ConfigMap with a target resource")
			driverCM := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      DriverCMName,
					Namespace: namespaceName,
					Annotations: map[string]string{
						TemplateWatchAnnotation: AnnotationValue,
					},
				},
				Data: map[string]string{
					ConfigDataKey: `
- name: my-cm
  template: |
    apiVersion: v1
    kind: ConfigMap
    metadata:
      name: ` + TargetCMName + `
    data:
      key: value
`,
				},
			}
			Expect(k8sClient.Create(context.Background(), driverCM)).Should(Succeed())

			By("Verifying the target ConfigMap is created")
			targetCM := &corev1.ConfigMap{}
			targetLookupKey := types.NamespacedName{Name: TargetCMName, Namespace: namespaceName}

			Eventually(func() bool {
				err := k8sClient.Get(context.Background(), targetLookupKey, targetCM)
				return err == nil
			}, Timeout, Interval).Should(BeTrue())

			Expect(targetCM.Data["key"]).Should(Equal("value"))

			By("Verifying the owner reference is set")
			Expect(targetCM.OwnerReferences).Should(HaveLen(1))
			Expect(targetCM.OwnerReferences[0].Name).Should(Equal(DriverCMName))
			Expect(targetCM.OwnerReferences[0].Kind).Should(Equal("ConfigMap"))

			By("Updating the driver ConfigMap to remove the target resource")
			var updatedDriverCM corev1.ConfigMap
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: DriverCMName, Namespace: namespaceName}, &updatedDriverCM)).Should(Succeed())

			updatedDriverCM.Data[ConfigDataKey] = "" // Empty the resources
			Expect(k8sClient.Update(context.Background(), &updatedDriverCM)).Should(Succeed())

			By("Verifying the target ConfigMap is deleted")
			Eventually(func() bool {
				err := k8sClient.Get(context.Background(), targetLookupKey, targetCM)
				return errors.IsNotFound(err)
			}, Timeout, Interval).Should(BeTrue())
		})
	})

	Context("When an immutable field is changed", func() {
		const TargetDeployName = "target-deployment"

		deploymentTemplate := func(selector, annotation string) string {
			return `
- name: my-deploy
  template: |
    apiVersion: apps/v1
    kind: Deployment
    metadata:
      name: ` + TargetDeployName + `
      ` + annotation + `
    spec:
      replicas: 1
      selector:
        matchLabels:
          app: ` + selector + `
      template:
        metadata:
          labels:
            app: ` + selector + `
        spec:
          containers:
          - name: nginx
            image: nginx
`
		}

		It("Should replace the resource only if the replace annotation is present", func() {
			By("Creating a driver ConfigMap with a Deployment resource")
			driverCM := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      DriverCMName,
					Namespace: namespaceName,
					Annotations: map[string]string{
						TemplateWatchAnnotation: AnnotationValue,
					},
				},
				Data: map[string]string{
					ConfigDataKey: deploymentTemplate("nginx", ""),
				},
			}
			Expect(k8sClient.Create(context.Background(), driverCM)).Should(Succeed())

			By("Verifying the Deployment is created")
			targetDeploy := &appsv1.Deployment{}
			targetLookupKey := types.NamespacedName{Name: TargetDeployName, Namespace: namespaceName}

			Eventually(func() error {
				return k8sClient.Get(context.Background(), targetLookupKey, targetDeploy)
			}, Timeout, Interval).Should(Succeed())

			initialUID := targetDeploy.UID
			Expect(initialUID).NotTo(BeEmpty())

			By("Updating the driver ConfigMap to change an immutable field (selector)")
			var updatedDriverCM corev1.ConfigMap
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: DriverCMName, Namespace: namespaceName}, &updatedDriverCM)).Should(Succeed())
			updatedDriverCM.Data[ConfigDataKey] = deploymentTemplate("nginx-new", "")
			Expect(k8sClient.Update(context.Background(), &updatedDriverCM)).Should(Succeed())

			By("Verifying the Deployment is NOT replaced (UID should be the same)")
			// We use Consistently because we expect nothing to happen
			Consistently(func() types.UID {
				err := k8sClient.Get(context.Background(), targetLookupKey, targetDeploy)
				if err != nil {
					return ""
				}
				return targetDeploy.UID
			}, time.Second*3, Interval).Should(Equal(initialUID))

			By("Updating the driver to add the replace annotation and change the immutable field again")
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: DriverCMName, Namespace: namespaceName}, &updatedDriverCM)).Should(Succeed())
			annotationYAML := "annotations:\n        " + ReplaceEnabledAnnotation + ": \"true\""
			updatedDriverCM.Data[ConfigDataKey] = deploymentTemplate("nginx-final", annotationYAML)
			Expect(k8sClient.Update(context.Background(), &updatedDriverCM)).Should(Succeed())

			By("Verifying the Deployment IS replaced and has the new field")
			Eventually(func() string {
				err := k8sClient.Get(context.Background(), targetLookupKey, targetDeploy)
				if err != nil {
					return ""
				}
				if targetDeploy.Spec.Selector == nil {
					return ""
				}
				return targetDeploy.Spec.Selector.MatchLabels["app"]
			}, Timeout, Interval).Should(Equal("nginx-final"))
		})
	})
})