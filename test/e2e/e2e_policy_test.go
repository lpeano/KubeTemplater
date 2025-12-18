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
package e2e

import (
	"bytes"
	"fmt"
	"os/exec"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/lpeano/KubeTemplater/test/utils"
)

var _ = Describe("KubeTemplatePolicy", func() {
	const (
		operatorNamespace = "default"
		sourceNamespace   = "source-ns"
		targetNamespace   = "target-ns"
		anotherNamespace  = "another-ns"
		policyName        = "test-policy"
		templateName      = "test-template"
		configMapName     = "test-cm"
	)

	BeforeEach(func() {
		for _, ns := range []string{sourceNamespace, targetNamespace, anotherNamespace} {
			cmd := exec.Command("kubectl", "create", "namespace", ns)
			utils.Run(cmd)
		}
	})

	AfterEach(func() {
		for _, ns := range []string{sourceNamespace, targetNamespace, anotherNamespace} {
			cmd := exec.Command("kubectl", "delete", "namespace", ns, "--ignore-not-found")
			utils.Run(cmd)
		}
		cmd := exec.Command("kubectl", "delete", "kubetemplatepolicy", policyName, "-n", operatorNamespace, "--ignore-not-found")
		utils.Run(cmd)
	})

	Context("when a KubeTemplate is created", func() {
		It("should fail if no matching policy is found", func() {
			By("creating a KubeTemplate in the source namespace")
			template := fmt.Sprintf(`
apiVersion: kubetemplater.io/v1alpha1
kind: KubeTemplate
metadata:
  name: %s
  namespace: %s
spec:
  templates:
    - object:
        apiVersion: v1
        kind: ConfigMap
        metadata:
          name: %s
          namespace: %s
        data:
          key: value
`, templateName, sourceNamespace, configMapName, targetNamespace)
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = bytes.NewBufferString(template)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("checking that the KubeTemplate is rejected by webhook or controller")
			// The webhook may reject it immediately, or the controller will set error status
			Eventually(func() string {
				cmd := exec.Command("kubectl", "get", "kubetemplate", templateName, "-n", sourceNamespace, "-o", "jsonpath={.status.status}")
				output, _ := utils.Run(cmd)
				return string(output)
			}, 2*time.Minute, 5*time.Second).Should(ContainSubstring("No KubeTemplatePolicy found for source namespace"))
		})
	})

	Context("when a matching policy exists", func() {
		BeforeEach(func() {
			policy := fmt.Sprintf(`
apiVersion: kubetemplater.io/v1alpha1
kind: KubeTemplatePolicy
metadata:
  name: %s
  namespace: %s
spec:
  sourceNamespace: %s
  validationRules:
    - kind: ConfigMap
      group: ""
      version: v1
      targetNamespaces:
        - %s
    - kind: Secret
      group: ""
      version: v1
      targetNamespaces: []
`, policyName, operatorNamespace, sourceNamespace, targetNamespace)
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = bytes.NewBufferString(policy)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should create a resource in an allowed target namespace", func() {
			By("creating a KubeTemplate that creates a ConfigMap in the target namespace")
			template := fmt.Sprintf(`
apiVersion: kubetemplater.io/v1alpha1
kind: KubeTemplate
metadata:
  name: %s
  namespace: %s
spec:
  templates:
    - object:
        apiVersion: v1
        kind: ConfigMap
        metadata:
          name: %s
          namespace: %s
        data:
          key: value
`, templateName, sourceNamespace, configMapName, targetNamespace)
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = bytes.NewBufferString(template)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("checking that the ConfigMap is created")
			Eventually(func() error {
				cmd := exec.Command("kubectl", "get", "configmap", configMapName, "-n", targetNamespace)
				_, err := utils.Run(cmd)
				return err
			}, 2*time.Minute, 5*time.Second).Should(Succeed())
		})

		It("should not create a resource in a disallowed target namespace", func() {
			By("creating a KubeTemplate that creates a ConfigMap in another namespace")
			template := fmt.Sprintf(`
apiVersion: kubetemplater.io/v1alpha1
kind: KubeTemplate
metadata:
  name: %s
  namespace: %s
spec:
  templates:
    - object:
        apiVersion: v1
        kind: ConfigMap
        metadata:
          name: %s
          namespace: %s
        data:
          key: value
`, templateName, sourceNamespace, configMapName, anotherNamespace)
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = bytes.NewBufferString(template)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("checking that the KubeTemplate status shows an error or webhook rejects it")
			Eventually(func() string {
				cmd := exec.Command("kubectl", "get", "kubetemplate", templateName, "-n", sourceNamespace, "-o", "jsonpath={.status.status}")
				output, _ := utils.Run(cmd)
				return string(output)
			}, 2*time.Minute, 5*time.Second).Should(ContainSubstring("is not an allowed target for resource"))

			By("checking that the ConfigMap is not created")
			Consistently(func() error {
				cmd := exec.Command("kubectl", "get", "configmap", configMapName, "-n", anotherNamespace)
				_, err := utils.Run(cmd)
				return err
			}, 45*time.Second, 5*time.Second).ShouldNot(Succeed())
		})

		It("should not create a resource if targetNamespaces is empty", func() {
			By("creating a KubeTemplate that creates a Secret")
			template := fmt.Sprintf(`
apiVersion: kubetemplater.io/v1alpha1
kind: KubeTemplate
metadata:
  name: %s
  namespace: %s
spec:
  templates:
    - object:
        apiVersion: v1
        kind: Secret
        metadata:
          name: my-secret
          namespace: %s
        stringData:
          key: value
`, templateName, sourceNamespace, targetNamespace)
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = bytes.NewBufferString(template)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("checking that the KubeTemplate status shows an error")
			Eventually(func() string {
				cmd := exec.Command("kubectl", "get", "kubetemplate", templateName, "-n", sourceNamespace, "-o", "jsonpath={.status.status}")
				output, _ := utils.Run(cmd)
				return string(output)
			}, 2*time.Minute, 5*time.Second).Should(ContainSubstring("no target namespaces defined"))
		})
	})

	Context("when dealing with immutable fields", func() {
		const (
			serviceName  = "test-service"
			templateName = "immutable-template"
		)

		BeforeEach(func() {
			policy := fmt.Sprintf(`
apiVersion: kubetemplater.io/v1alpha1
kind: KubeTemplatePolicy
metadata:
  name: %s
  namespace: %s
spec:
  sourceNamespace: %s
  validationRules:
    - kind: Service
      group: ""
      version: v1
      targetNamespaces:
        - %s
`, policyName, operatorNamespace, sourceNamespace, sourceNamespace) // for simplicity, source and target are the same
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = bytes.NewBufferString(policy)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should fail to update an immutable field if replace is false", func() {
			By("creating a Service")
			template := fmt.Sprintf(`
apiVersion: kubetemplater.io/v1alpha1
kind: KubeTemplate
metadata:
  name: %s
  namespace: %s
spec:
  templates:
    - object:
        apiVersion: v1
        kind: Service
        metadata:
          name: %s
        spec:
          ports:
          - port: 80
`, templateName, sourceNamespace, serviceName)
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = bytes.NewBufferString(template)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			var originalClusterIP string
			By("ensuring the service is created and getting its ClusterIP")
			Eventually(func() error {
				cmd := exec.Command("kubectl", "get", "service", serviceName, "-n", sourceNamespace, "-o", "jsonpath={.spec.clusterIP}")
				output, err := utils.Run(cmd)
				if err != nil {
					return err
				}
				originalClusterIP = string(output)
				return nil
			}, time.Minute, 5*time.Second).Should(Succeed())

			By("updating the template to change an immutable field")
			updatedTemplate := fmt.Sprintf(`
apiVersion: kubetemplater.io/v1alpha1
kind: KubeTemplate
metadata:
  name: %s
  namespace: %s
spec:
  templates:
    - object:
        apiVersion: v1
        kind: Service
        metadata:
          name: %s
        spec:
          clusterIP: "" # attempting to change immutable field
          ports:
          - port: 80
`, templateName, sourceNamespace, serviceName)
			cmd = exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = bytes.NewBufferString(updatedTemplate)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("checking that the KubeTemplate status shows an error")
			Eventually(func() string {
				cmd := exec.Command("kubectl", "get", "kubetemplate", templateName, "-n", sourceNamespace, "-o", "jsonpath={.status.status}")
				output, _ := utils.Run(cmd)
				return string(output)
			}, 2*time.Minute, 5*time.Second).Should(ContainSubstring("Failed to apply object"))

			By("checking that the service's clusterIP has not changed")
			Consistently(func() string {
				cmd := exec.Command("kubectl", "get", "service", serviceName, "-n", sourceNamespace, "-o", "jsonpath={.spec.clusterIP}")
				output, _ := utils.Run(cmd)
				return string(output)
			}, 45*time.Second, 5*time.Second).Should(Equal(originalClusterIP))
		})

		It("should replace the object if replace is true", func() {
			By("creating a Service")
			template := fmt.Sprintf(`
apiVersion: kubetemplater.io/v1alpha1
kind: KubeTemplate
metadata:
  name: %s
  namespace: %s
spec:
  templates:
    - replace: true # Key change for this test
      object:
        apiVersion: v1
        kind: Service
        metadata:
          name: %s
        spec:
          ports:
          - port: 80
`, templateName, sourceNamespace, serviceName)
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = bytes.NewBufferString(template)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			var originalClusterIP string
			By("ensuring the service is created and getting its ClusterIP")
			Eventually(func() error {
				cmd := exec.Command("kubectl", "get", "service", serviceName, "-n", sourceNamespace, "-o", "jsonpath={.spec.clusterIP}")
				output, err := utils.Run(cmd)
				if err != nil {
					return err
				}
				originalClusterIP = string(output)
				return nil
			}, 2*time.Minute, 5*time.Second).Should(Succeed())

			By("updating the template to change an immutable field")
			updatedTemplate := fmt.Sprintf(`
apiVersion: kubetemplater.io/v1alpha1
kind: KubeTemplate
metadata:
  name: %s
  namespace: %s
spec:
  templates:
    - replace: true # Key change for this test
      object:
        apiVersion: v1
        kind: Service
        metadata:
          name: %s
        spec:
          clusterIP: "" # attempting to change immutable field
          ports:
          - port: 80
`, templateName, sourceNamespace, serviceName)
			cmd = exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = bytes.NewBufferString(updatedTemplate)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("checking that the service's clusterIP has changed")
			Eventually(func() string {
				cmd := exec.Command("kubectl", "get", "service", serviceName, "-n", sourceNamespace, "-o", "jsonpath={.spec.clusterIP}")
				output, _ := utils.Run(cmd)
				return string(output)
			}, 2*time.Minute, 5*time.Second).ShouldNot(Equal(originalClusterIP))
		})
	})
})
