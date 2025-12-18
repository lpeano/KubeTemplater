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

package cache

import (
	"context"
	"fmt"
	"sync"
	"time"

	kubetemplateriov1alpha1 "github.com/lpeano/KubeTemplater/api/kubetemplater.io/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// DefaultTTL is the default time-to-live for cache entries
	DefaultTTL = 5 * time.Minute
)

// PolicyCache provides a thread-safe cache for KubeTemplatePolicies indexed by source namespace
type PolicyCache struct {
	mu      sync.RWMutex
	entries map[string]*cacheEntry
	ttl     time.Duration
	client  client.Client
}

type cacheEntry struct {
	policy    *kubetemplateriov1alpha1.KubeTemplatePolicy
	expiresAt time.Time
}

// NewPolicyCache creates a new PolicyCache
func NewPolicyCache(client client.Client, ttl time.Duration) *PolicyCache {
	if ttl == 0 {
		ttl = DefaultTTL
	}
	return &PolicyCache{
		entries: make(map[string]*cacheEntry),
		ttl:     ttl,
		client:  client,
	}
}

// Get retrieves a policy from the cache by source namespace
// If the entry is expired or not found, it fetches from the API server and updates the cache
func (c *PolicyCache) Get(ctx context.Context, sourceNamespace string, operatorNamespace string) (*kubetemplateriov1alpha1.KubeTemplatePolicy, error) {
	log := logf.FromContext(ctx)

	// Try to get from cache first
	c.mu.RLock()
	entry, found := c.entries[sourceNamespace]
	c.mu.RUnlock()

	if found && time.Now().Before(entry.expiresAt) {
		log.V(1).Info("Policy cache hit", "sourceNamespace", sourceNamespace)
		// If policy is nil in cache, it means "not found" was cached
		if entry.policy == nil {
			return nil, fmt.Errorf("no KubeTemplatePolicy found for source namespace %s", sourceNamespace)
		}
		return entry.policy, nil
	}

	// Cache miss or expired - fetch from API server
	log.V(1).Info("Policy cache miss", "sourceNamespace", sourceNamespace)
	return c.refresh(ctx, sourceNamespace, operatorNamespace)
}

// refresh fetches the policy from the API server and updates the cache
func (c *PolicyCache) refresh(ctx context.Context, sourceNamespace string, operatorNamespace string) (*kubetemplateriov1alpha1.KubeTemplatePolicy, error) {
	var policies kubetemplateriov1alpha1.KubeTemplatePolicyList
	if err := c.client.List(ctx, &policies,
		client.InNamespace(operatorNamespace),
		client.MatchingFields{"spec.sourceNamespace": sourceNamespace}); err != nil {
		return nil, fmt.Errorf("failed to list KubeTemplatePolicies: %w", err)
	}

	if len(policies.Items) > 1 {
		return nil, fmt.Errorf("multiple KubeTemplatePolicies found for source namespace %s", sourceNamespace)
	}

	if len(policies.Items) == 0 {
		// Cache the "not found" result to avoid repeated API calls
		c.mu.Lock()
		c.entries[sourceNamespace] = &cacheEntry{
			policy:    nil,
			expiresAt: time.Now().Add(c.ttl),
		}
		c.mu.Unlock()
		return nil, fmt.Errorf("no KubeTemplatePolicy found for source namespace %s", sourceNamespace)
	}

	policy := &policies.Items[0]

	// Update cache
	c.mu.Lock()
	c.entries[sourceNamespace] = &cacheEntry{
		policy:    policy,
		expiresAt: time.Now().Add(c.ttl),
	}
	c.mu.Unlock()

	return policy, nil
}

// Set explicitly sets a policy in the cache (used by the cache controller on watch events)
func (c *PolicyCache) Set(sourceNamespace string, policy *kubetemplateriov1alpha1.KubeTemplatePolicy) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries[sourceNamespace] = &cacheEntry{
		policy:    policy,
		expiresAt: time.Now().Add(c.ttl),
	}
}

// Delete removes a policy from the cache
func (c *PolicyCache) Delete(sourceNamespace string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.entries, sourceNamespace)
}

// Clear removes all entries from the cache
func (c *PolicyCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries = make(map[string]*cacheEntry)
}

// Size returns the current number of entries in the cache
func (c *PolicyCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return len(c.entries)
}

// Invalidate removes a specific entry from the cache by source namespace
func (c *PolicyCache) Invalidate(sourceNamespace string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.entries, sourceNamespace)
}

// Update immediately updates the cache with a new or modified policy
func (c *PolicyCache) Update(policy *kubetemplateriov1alpha1.KubeTemplatePolicy) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries[policy.Spec.SourceNamespace] = &cacheEntry{
		policy:    policy,
		expiresAt: time.Now().Add(c.ttl),
	}
}
