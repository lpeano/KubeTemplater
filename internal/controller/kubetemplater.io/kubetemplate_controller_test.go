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

package kubetemplaterio

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubetemplateriov1alpha1 "github.com/lpeano/KubeTemplater/api/kubetemplater.io/v1alpha1"
)

var _ = Describe("KubeTemplate Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"
		const policyName = "test-policy"
		const operatorNamespace = "default" // In real deployment it would be kubetemplater-system

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}
		policyNamespacedName := types.NamespacedName{
			Name:      policyName,
			Namespace: operatorNamespace,
		}
		kubetemplate := &kubetemplateriov1alpha1.KubeTemplate{}

		BeforeEach(func() {
			By("creating a KubeTemplatePolicy for the source namespace")
			policy := &kubetemplateriov1alpha1.KubeTemplatePolicy{}
			err := k8sClient.Get(ctx, policyNamespacedName, policy)
			if err != nil && errors.IsNotFound(err) {
				policy = &kubetemplateriov1alpha1.KubeTemplatePolicy{
					ObjectMeta: metav1.ObjectMeta{
						Name:      policyName,
						Namespace: operatorNamespace,
					},
					Spec: kubetemplateriov1alpha1.KubeTemplatePolicySpec{
						SourceNamespace: "default",
						ValidationRules: []kubetemplateriov1alpha1.ValidationRule{
							{
								Kind:             "ConfigMap",
								Group:            "",
								Version:          "v1",
								TargetNamespaces: []string{"default"},
							},
						},
					},
				}
				Expect(k8sClient.Create(ctx, policy)).To(Succeed())
			}

			By("creating the custom resource for the Kind KubeTemplate")
			err = k8sClient.Get(ctx, typeNamespacedName, kubetemplate)
			if err != nil && errors.IsNotFound(err) {
				resource := &kubetemplateriov1alpha1.KubeTemplate{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					Spec: kubetemplateriov1alpha1.KubeTemplateSpec{
						Templates: []kubetemplateriov1alpha1.Template{
							{
								Object: runtime.RawExtension{
									Raw: []byte(`{
  "apiVersion": "v1",
  "kind": "ConfigMap",
  "metadata": {
    "name": "test-cm",
    "namespace": "default"
  },
  "data": {
    "key": "value"
  }
}`),
								},
							},
						},
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			By("Cleanup the specific resource instance KubeTemplate")
			resource := &kubetemplateriov1alpha1.KubeTemplate{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}

			By("Cleanup the KubeTemplatePolicy")
			policy := &kubetemplateriov1alpha1.KubeTemplatePolicy{}
			err = k8sClient.Get(ctx, policyNamespacedName, policy)
			if err == nil {
				Expect(k8sClient.Delete(ctx, policy)).To(Succeed())
			}
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &KubeTemplateReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			// TODO(user): Add more specific assertions depending on your controller's reconciliation logic.
			// Example: If you expect a certain status condition after reconciliation, verify it here.
		})
	})
})
