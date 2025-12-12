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
	"crypto/tls"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var secretLog = logf.Log.WithName("secret-cert-watcher")

// SecretCertWatcher watches a Kubernetes Secret and serves the certificate it contains.
type SecretCertWatcher struct {
	Client          client.Client         // For Get operations (public for external assignment)
	clientset       *kubernetes.Clientset // For Watch operations
	secretName      string
	secretNamespace string
	cert            atomic.Value // Holds the current *tls.Certificate
	isReady         chan struct{}
	readyOnce       sync.Once
	lastValidCert   *tls.Certificate // Keep last valid cert for graceful rotation
	lastCertMu      sync.RWMutex     // Protect lastValidCert
}

// NewSecretCertWatcher creates a new SecretCertWatcher.
func NewSecretCertWatcher(client client.Client, clientset *kubernetes.Clientset, secretName, secretNamespace string) *SecretCertWatcher {
	return &SecretCertWatcher{
		Client:          client,
		clientset:       clientset,
		secretName:      secretName,
		secretNamespace: secretNamespace,
		isReady:         make(chan struct{}),
	}
}

// NeedLeaderElection implements manager.LeaderElectionRunnable
// Returns false because all pods need to watch and load certificates, not just the leader
func (s *SecretCertWatcher) NeedLeaderElection() bool {
	return false
}

// GetCertificate is the callback for tls.Config.GetCertificate.
func (s *SecretCertWatcher) GetCertificate(_ *tls.ClientHelloInfo) (*tls.Certificate, error) {
	<-s.isReady // Block until the first certificate is loaded
	cert, ok := s.cert.Load().(*tls.Certificate)
	if ok && cert != nil {
		return cert, nil
	}
	
	// Certificate is nil (deleted), try to return last valid cert for graceful rotation
	s.lastCertMu.RLock()
	lastCert := s.lastValidCert
	s.lastCertMu.RUnlock()
	
	if lastCert != nil {
		secretLog.V(1).Info("Returning last valid certificate during rotation")
		return lastCert, nil
	}
	
	return nil, fmt.Errorf("certificate not loaded or invalid type")
}

// Start begins the process of watching the secret.
// This is a blocking call and should be run in a goroutine.
func (s *SecretCertWatcher) Start(ctx context.Context) error {
	secretLog.Info("Starting secret certificate watcher", "secret", s.secretName, "namespace", s.secretNamespace)

	// Initial load
	s.performInitialLoad(ctx)

	// Watch loop with backoff on failures
	for {
		select {
		case <-ctx.Done():
			secretLog.Info("Context cancelled, stopping secret watcher.")
			return nil
		default:
			s.runWatch(ctx)
			// Add small delay between watch restarts to avoid busy loop on persistent errors
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(2 * time.Second):
				// Continue to next watch attempt
			}
		}
	}
}

// performInitialLoad tries to load the secret once at the beginning.
// If it fails, the watch will pick it up later.
func (s *SecretCertWatcher) performInitialLoad(ctx context.Context) {
	initialLoadCtx, cancel := context.WithTimeout(ctx, 1*time.Minute)
	defer cancel()

	secretLog.Info("Performing initial load of certificate secret")
	var secret *corev1.Secret
	var err error

	// Ensure isReady is closed even if we fail to load
	defer func() {
		s.readyOnce.Do(func() {
			close(s.isReady)
			secretLog.Info("Certificate watcher is ready (may not have cert yet)")
		})
	}()

	// Retry loop for initial load
	for {
		secret, err = s.clientset.CoreV1().Secrets(s.secretNamespace).Get(initialLoadCtx, s.secretName, metav1.GetOptions{})
		if err == nil {
			secretLog.Info("Initial secret found, loading certificate")
			if err := s.loadCertificate(secret); err != nil {
				secretLog.Error(err, "Failed to load certificate from initial secret")
			}
			return // Success
		}

		if errors.IsNotFound(err) {
			secretLog.Info("Secret not found on initial load, will wait for it to be created...")
		} else {
			secretLog.Error(err, "Failed to get secret on initial load")
		}

		select {
		case <-initialLoadCtx.Done():
			secretLog.Info("Initial load context finished, watch will continue looking for secret.")
			// Don't error out, let the watch handle it.
			return
		case <-time.After(5 * time.Second):
			// continue retrying
		}
	}
}

// runWatch sets up and manages the watch on the secret.
func (s *SecretCertWatcher) runWatch(ctx context.Context) {
	watcher, err := s.clientset.CoreV1().Secrets(s.secretNamespace).Watch(ctx, metav1.ListOptions{
		FieldSelector: fmt.Sprintf("metadata.name=%s", s.secretName),
	})
	if err != nil {
		secretLog.Error(err, "Failed to create secret watch, retrying in 5 seconds")
		time.Sleep(5 * time.Second)
		return
	}
	defer watcher.Stop()

	secretLog.Info("Secret watch established")
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-watcher.ResultChan():
			if !ok {
				secretLog.Info("Secret watch channel closed, restarting watch in 2 seconds.")
				time.Sleep(2 * time.Second) // Prevent busy loop
				return
			}
			
			if event.Type == watch.Deleted {
				secretLog.Info("Secret was deleted. Keeping last certificate for graceful rotation.")
				// Don't clear cert - keep serving last valid cert until new one arrives
				// This prevents downtime during rotation
				continue
			}

			secret, ok := event.Object.(*corev1.Secret)
			if !ok {
				secretLog.Info("Received non-secret object in watch stream")
				continue
			}

			if err := s.loadCertificate(secret); err != nil {
				secretLog.Error(err, "Failed to process secret from watch event")
			}
		}
	}
}

// loadCertificate parses a secret and updates the in-memory certificate.
func (s *SecretCertWatcher) loadCertificate(secret *corev1.Secret) error {
	certPEM, ok := secret.Data["tls.crt"]
	if !ok {
		return fmt.Errorf("secret %s is missing tls.crt", s.secretName)
	}
	keyPEM, ok := secret.Data["tls.key"]
	if !ok {
		return fmt.Errorf("secret %s is missing tls.key", s.secretName)
	}

	// Check if certificate data is empty (race condition during secret creation)
	if len(certPEM) == 0 || len(keyPEM) == 0 {
		secretLog.V(1).Info("Secret exists but certificate data is empty, waiting for CertManager to populate it",
			"secret", s.secretName,
			"certLength", len(certPEM),
			"keyLength", len(keyPEM))
		return fmt.Errorf("certificate data not yet populated in secret %s", s.secretName)
	}

	// Debug logging
	secretLog.V(1).Info("Loading certificate from secret",
		"secret", s.secretName,
		"certLength", len(certPEM),
		"keyLength", len(keyPEM),
		"certFirst20", string(certPEM[:min(20, len(certPEM))]),
		"keyFirst20", string(keyPEM[:min(20, len(keyPEM))]))

	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		secretLog.Error(err, "X509KeyPair failed",
			"certLength", len(certPEM),
			"keyLength", len(keyPEM))
		return fmt.Errorf("failed to parse certificate and key from secret %s: %w", s.secretName, err)
	}

	s.cert.Store(&cert)
	
	// Save as last valid certificate
	s.lastCertMu.Lock()
	s.lastValidCert = &cert
	s.lastCertMu.Unlock()
	
	// Close isReady on first successful load (handled by performInitialLoad defer)
	s.readyOnce.Do(func() {
		close(s.isReady)
		secretLog.Info("Certificate loaded and watcher is ready")
	})

	secretLog.Info("Successfully reloaded certificate from secret", "secret", s.secretName)
	return nil
}

// IsReady returns true if the certificate has been loaded at least once.
func (s *SecretCertWatcher) IsReady() bool {
	select {
	case <-s.isReady:
		return true
	default:
		return false
	}
}