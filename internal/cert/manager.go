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

package cert

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"time"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var log = logf.Log.WithName("cert-manager")

const (
	// Certificate validity periods
	CAValidityDuration       = 10 * 365 * 24 * time.Hour // 10 years for CA
	CertValidityDuration     = 365 * 24 * time.Hour      // 1 year for server cert
	// Renew certificate when it has less than this time remaining
	RenewThreshold = 30 * 24 * time.Hour // 30 days
	// Renew CA when it has less than this time remaining (longer period for CA)
	CARenewThreshold = 365 * 24 * time.Hour // 1 year before CA expiration
	// Check interval for certificate renewal
	CheckInterval = 24 * time.Hour // Daily check
)

// Manager manages webhook certificates with persistent CA
type Manager struct {
	client                  client.Client
	clientset               *kubernetes.Clientset
	secretName              string
	secretNamespace         string
	serviceName             string
	webhookConfigName       string
	stopCh                  chan struct{}
	started                 bool
}

// NewManager creates a new certificate manager
func NewManager(client client.Client, clientset *kubernetes.Clientset, secretName, secretNamespace, serviceName, webhookConfigName string) *Manager {
	return &Manager{
		client:            client,
		clientset:         clientset,
		secretName:        secretName,
		secretNamespace:   secretNamespace,
		serviceName:       serviceName,
		webhookConfigName: webhookConfigName,
		stopCh:            make(chan struct{}),
		started:           false,
	}
}

// NeedLeaderElection implements manager.LeaderElectionRunnable
func (m *Manager) NeedLeaderElection() bool {
	return true
}

// Start starts the certificate manager (should be called only by leader)
// This is called by the manager when this instance becomes the leader
func (m *Manager) Start(ctx context.Context) error {
	if m.started {
		log.Info("Certificate manager already started, skipping")
		return nil
	}
	m.started = true

	log.Info("Starting certificate manager (leader instance)", "secretName", m.secretName, "namespace", m.secretNamespace)

	// Initial certificate check and generation
	if err := m.ensureCertificate(ctx); err != nil {
		return fmt.Errorf("failed to ensure certificate: %w", err)
	}

	// Start renewal loop
	go m.renewalLoop(ctx)

	return nil
}

// Stop stops the certificate manager
func (m *Manager) Stop() {
	close(m.stopCh)
}

// renewalLoop periodically checks and renews certificates
func (m *Manager) renewalLoop(ctx context.Context) {
	ticker := time.NewTicker(CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := m.ensureCertificate(ctx); err != nil {
				log.Error(err, "Failed to ensure certificate during renewal check")
			}
		case <-m.stopCh:
			log.Info("Certificate renewal loop stopped")
			return
		case <-ctx.Done():
			log.Info("Certificate renewal loop stopped due to context cancellation")
			return
		}
	}
}

// ensureCertificate checks if certificate exists and is valid, generates if needed
func (m *Manager) ensureCertificate(ctx context.Context) error {
	// Ensure CA certificate exists first
	caCert, caKey, err := m.ensureCA(ctx)
	if err != nil {
		return fmt.Errorf("failed to ensure CA: %w", err)
	}

	// Check if server certificate needs generation
	needsGeneration, err := m.needsServerCertGeneration(ctx, caCert)
	if err != nil {
		return fmt.Errorf("failed to check server certificate: %w", err)
	}

	if needsGeneration {
		if err := m.generateServerCert(ctx, caCert, caKey); err != nil {
			return fmt.Errorf("failed to generate server certificate: %w", err)
		}

		// Patch ValidatingWebhookConfiguration with CA bundle
		if err := m.patchWebhookConfiguration(ctx, caCert); err != nil {
			log.Error(err, "Failed to patch webhook configuration", "note", "Webhook may not work correctly")
			// Don't fail - certificate is still valid
		}
	}

	return nil
}

// ensureCA ensures the CA certificate exists, creates if needed, and handles CA renewal with coexistence period
func (m *Manager) ensureCA(ctx context.Context) (*x509.Certificate, *rsa.PrivateKey, error) {
	caSecretName := m.secretName + "-ca"
	caSecretNameNew := caSecretName + "-new"
	
	// Check if new CA exists (in transition period)
	newSecret := &corev1.Secret{}
	err := m.client.Get(ctx, types.NamespacedName{
		Name:      caSecretNameNew,
		Namespace: m.secretNamespace,
	}, newSecret)
	
	if err == nil {
		// New CA exists, check if old CA has expired
		newCACert, newCAKey, err := m.parseCAFromSecret(newSecret)
		if err != nil {
			log.Error(err, "Failed to parse new CA, will try current CA")
		} else {
			// Check if old CA exists and if it's expired
			oldSecret := &corev1.Secret{}
			oldErr := m.client.Get(ctx, types.NamespacedName{
				Name:      caSecretName,
				Namespace: m.secretNamespace,
			}, oldSecret)
			
			if oldErr == nil {
				oldCACert, _, parseErr := m.parseCAFromSecret(oldSecret)
				if parseErr == nil && time.Now().After(oldCACert.NotAfter) {
					// Old CA has expired, promote new CA to primary
					log.Info("Old CA expired, promoting new CA to primary", 
						"oldExpiry", oldCACert.NotAfter,
						"newExpiry", newCACert.NotAfter)
					
					// Delete old CA secret
					if err := m.client.Delete(ctx, oldSecret); err != nil {
						log.Error(err, "Failed to delete old CA secret during promotion")
					}
					
					// Rename new CA to primary
					newSecret.ObjectMeta = metav1.ObjectMeta{
						Name:      caSecretName,
						Namespace: m.secretNamespace,
					}
					if err := m.client.Create(ctx, newSecret); err != nil {
						log.Error(err, "Failed to create promoted CA secret")
						return newCACert, newCAKey, nil // Use new CA anyway
					}
					
					// Delete the -new secret
					if err := m.client.Delete(ctx, &corev1.Secret{
						ObjectMeta: metav1.ObjectMeta{
							Name:      caSecretNameNew,
							Namespace: m.secretNamespace,
						},
					}); err != nil {
						log.Error(err, "Failed to delete new CA secret after promotion")
					}
					
					log.Info("CA promotion completed successfully")
				}
			}
			// Use new CA during transition period
			log.Info("Using new CA during transition period", "expiresAt", newCACert.NotAfter)
			return newCACert, newCAKey, nil
		}
	}
	
	// Check current CA
	secret := &corev1.Secret{}
	err = m.client.Get(ctx, types.NamespacedName{
		Name:      caSecretName,
		Namespace: m.secretNamespace,
	}, secret)

	if err == nil {
		// CA secret exists, parse it
		caCert, caKey, parseErr := m.parseCAFromSecret(secret)
		if parseErr != nil {
			return nil, nil, parseErr
		}
		
		// Check if CA needs renewal
		renewTime := time.Now().Add(CARenewThreshold)
		if caCert.NotAfter.Before(renewTime) {
			log.Info("CA certificate approaching expiration, generating new CA for coexistence period",
				"currentExpiry", caCert.NotAfter,
				"renewThreshold", renewTime,
				"daysRemaining", int(time.Until(caCert.NotAfter).Hours()/24))
			
			// Generate new CA with -new suffix
			newCACert, newCAKey, err := m.generateCA(ctx, caSecretNameNew)
			if err != nil {
				log.Error(err, "Failed to generate new CA, continuing with current CA")
				return caCert, caKey, nil
			}
			
			log.Info("New CA generated, now in coexistence period",
				"oldExpiry", caCert.NotAfter,
				"newExpiry", newCACert.NotAfter)
			
			// Return new CA for signing new certificates
			return newCACert, newCAKey, nil
		}
		
		return caCert, caKey, nil
	}

	if !errors.IsNotFound(err) {
		return nil, nil, fmt.Errorf("failed to get CA secret: %w", err)
	}

	// CA doesn't exist, generate new one
	log.Info("CA certificate not found, generating new CA")
	return m.generateCA(ctx, caSecretName)
}

// parseCAFromSecret parses CA certificate and key from secret
func (m *Manager) parseCAFromSecret(secret *corev1.Secret) (*x509.Certificate, *rsa.PrivateKey, error) {
	certPEM, ok := secret.Data["ca.crt"]
	if !ok {
		return nil, nil, fmt.Errorf("CA secret missing ca.crt")
	}
	keyPEM, ok := secret.Data["ca.key"]
	if !ok {
		return nil, nil, fmt.Errorf("CA secret missing ca.key")
	}

	certBlock, _ := pem.Decode(certPEM)
	if certBlock == nil {
		return nil, nil, fmt.Errorf("failed to decode CA certificate PEM")
	}
	caCert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse CA certificate: %w", err)
	}

	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		return nil, nil, fmt.Errorf("failed to decode CA key PEM")
	}
	caKey, err := x509.ParsePKCS1PrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse CA key: %w", err)
	}

	log.Info("Loaded existing CA certificate", "expiresAt", caCert.NotAfter)
	return caCert, caKey, nil
}

// generateCA generates a new CA certificate
func (m *Manager) generateCA(ctx context.Context, caSecretName string) (*x509.Certificate, *rsa.PrivateKey, error) {
	// Generate CA private key
	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate CA key: %w", err)
	}

	// Create CA certificate template
	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate serial number: %w", err)
	}

	caTemplate := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName:   "KubeTemplater CA",
			Organization: []string{"KubeTemplater"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(CAValidityDuration),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	// Self-sign the CA certificate
	caCertBytes, err := x509.CreateCertificate(rand.Reader, &caTemplate, &caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create CA certificate: %w", err)
	}

	caCert, err := x509.ParseCertificate(caCertBytes)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse generated CA certificate: %w", err)
	}

	// Store CA certificate in secret
	caCertPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caCertBytes})
	caKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(caKey)})

	caSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      caSecretName,
			Namespace: m.secretNamespace,
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"ca.crt": caCertPEM,
			"ca.key": caKeyPEM,
		},
	}

	if err := m.client.Create(ctx, caSecret); err != nil {
		return nil, nil, fmt.Errorf("failed to create CA secret: %w", err)
	}

	log.Info("CA certificate generated and stored", "validUntil", caTemplate.NotAfter)
	return caCert, caKey, nil
}

// needsServerCertGeneration checks if server certificate needs generation/renewal
func (m *Manager) needsServerCertGeneration(ctx context.Context, caCert *x509.Certificate) (bool, error) {
	secret := &corev1.Secret{}
	err := m.client.Get(ctx, types.NamespacedName{
		Name:      m.secretName,
		Namespace: m.secretNamespace,
	}, secret)

	if err != nil {
		if errors.IsNotFound(err) {
			log.Info("Certificate not found in secret, generating new certificate")
			return true, nil
		}
		return false, fmt.Errorf("failed to get secret: %w", err)
	}

	// Check if secret has certificate data
	certData, hasCert := secret.Data["tls.crt"]
	if !hasCert || len(certData) == 0 {
		log.Info("Certificate data missing in secret, regenerating")
		return true, nil
	}

	// Parse and validate certificate
	block, _ := pem.Decode(certData)
	if block == nil {
		log.Info("Invalid certificate PEM in secret, regenerating")
		return true, nil
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		log.Info("Failed to parse certificate, regenerating", "error", err)
		return true, nil
	}

	// Check if certificate expires soon
	renewTime := time.Now().Add(RenewThreshold)
	if cert.NotAfter.Before(renewTime) {
		log.Info("Certificate expires soon, renewing",
			"expiresAt", cert.NotAfter,
			"renewThreshold", renewTime)
		return true, nil
	}

	// Verify certificate is signed by current CA or old CA (during transition)
	roots := x509.NewCertPool()
	roots.AddCert(caCert)
	
	// During CA transition, also accept certificates signed by old CA
	caSecretName := m.secretName + "-ca"
	oldCASecret := &corev1.Secret{}
	if err := m.client.Get(ctx, types.NamespacedName{
		Name:      caSecretName,
		Namespace: m.secretNamespace,
	}, oldCASecret); err == nil {
		// Old CA exists, check if it's different from current CA
		oldCACert, _, parseErr := m.parseCAFromSecret(oldCASecret)
		if parseErr == nil && !oldCACert.Equal(caCert) {
			// Different CA, we're in transition period - accept both
			roots.AddCert(oldCACert)
			log.V(1).Info("CA transition detected, accepting certificates from both CAs",
				"currentCAExpiry", caCert.NotAfter,
				"oldCAExpiry", oldCACert.NotAfter)
		}
	}
	
	opts := x509.VerifyOptions{
		Roots: roots,
	}
	if _, err := cert.Verify(opts); err != nil {
		log.Info("Certificate not signed by any trusted CA, regenerating", "error", err)
		return true, nil
	}

	log.V(1).Info("Certificate is valid",
		"expiresAt", cert.NotAfter,
		"daysRemaining", int(time.Until(cert.NotAfter).Hours()/24))
	return false, nil
}

// generateServerCert generates a new server certificate signed by CA
func (m *Manager) generateServerCert(ctx context.Context, caCert *x509.Certificate, caKey *rsa.PrivateKey) error {
	log.Info("Generating new server certificate", "service", m.serviceName, "namespace", m.secretNamespace)

	// Generate server private key
	serverKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return fmt.Errorf("failed to generate server key: %w", err)
	}

	// Create server certificate template
	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return fmt.Errorf("failed to generate serial number: %w", err)
	}

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName:   fmt.Sprintf("%s.%s.svc", m.serviceName, m.secretNamespace),
			Organization: []string{"KubeTemplater"},
		},
		DNSNames: []string{
			m.serviceName,
			fmt.Sprintf("%s.%s", m.serviceName, m.secretNamespace),
			fmt.Sprintf("%s.%s.svc", m.serviceName, m.secretNamespace),
			fmt.Sprintf("%s.%s.svc.cluster.local", m.serviceName, m.secretNamespace),
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(CertValidityDuration),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	// Sign certificate with CA
	certBytes, err := x509.CreateCertificate(rand.Reader, &template, caCert, &serverKey.PublicKey, caKey)
	if err != nil {
		return fmt.Errorf("failed to create certificate: %w", err)
	}

	// Encode to PEM
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certBytes})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(serverKey)})

	// Update or create secret
	secret := &corev1.Secret{}
	err = m.client.Get(ctx, types.NamespacedName{
		Name:      m.secretName,
		Namespace: m.secretNamespace,
	}, secret)

	if err != nil {
		if !errors.IsNotFound(err) {
			return fmt.Errorf("failed to get secret: %w", err)
		}
		// Create new secret
		secret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      m.secretName,
				Namespace: m.secretNamespace,
			},
			Type: corev1.SecretTypeTLS,
			Data: map[string][]byte{
				"tls.crt": certPEM,
				"tls.key": keyPEM,
			},
		}
		if err := m.client.Create(ctx, secret); err != nil {
			return fmt.Errorf("failed to create secret: %w", err)
		}
		log.Info("Secret created successfully", "secretName", m.secretName)
	} else {
		// Update existing secret
		secret.Data = map[string][]byte{
			"tls.crt": certPEM,
			"tls.key": keyPEM,
		}
		if err := m.client.Update(ctx, secret); err != nil {
			return fmt.Errorf("failed to update secret: %w", err)
		}
		log.Info("Secret updated successfully", "secretName", m.secretName)
	}

	log.Info("Certificate generated and stored successfully",
		"secretName", m.secretName,
		"validUntil", template.NotAfter.Format(time.RFC3339))

	return nil
}

// patchWebhookConfiguration updates the ValidatingWebhookConfiguration with CA bundle
func (m *Manager) patchWebhookConfiguration(ctx context.Context, caCert *x509.Certificate) error {
	log.Info("Patching validating webhook configuration with new CA bundle", "name", m.webhookConfigName)

	caCertPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caCert.Raw})

	webhookConfig := &admissionv1.ValidatingWebhookConfiguration{}
	err := m.client.Get(ctx, types.NamespacedName{Name: m.webhookConfigName}, webhookConfig)
	if err != nil {
		return fmt.Errorf("failed to get webhook configuration: %w", err)
	}

	// Update CA bundle for all webhooks
	for i := range webhookConfig.Webhooks {
		webhookConfig.Webhooks[i].ClientConfig.CABundle = caCertPEM
	}

	if err := m.client.Update(ctx, webhookConfig); err != nil {
		return fmt.Errorf("failed to update webhook configuration: %w", err)
	}

	log.Info("Successfully patched ValidatingWebhookConfiguration with new CA bundle", "name", m.webhookConfigName)
	return nil
}