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

package queue

import (
	"container/heap"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/types"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// Default retry configuration values
const (
	DefaultMaxRetries        = 5
	DefaultInitialRetryDelay = 1 * time.Second
	DefaultMaxRetryDelay     = 5 * time.Minute
	DefaultMaxRetryCycles    = 3 // Default: stop after 3 full retry cycles (configurable per WorkQueue)
)

// WorkItem represents a unit of work to be processed
type WorkItem struct {
	NamespacedName types.NamespacedName
	Priority       int
	RetryCount     int
	RetryCycle     int       // Number of retry cycles (resets every MaxRetries)
	EnqueuedAt     time.Time
	ScheduledAt    time.Time // For delayed retries
	index          int       // Index in the priority queue
}

// WorkQueue is a thread-safe priority queue with retry logic
type WorkQueue struct {
	mu                sync.Mutex
	items             priorityQueue
	itemsMap          map[types.NamespacedName]*WorkItem
	cond              *sync.Cond
	shutdown          bool
	metrics           *QueueMetrics
	MaxRetries        int
	InitialRetryDelay time.Duration
	MaxRetryDelay     time.Duration
	MaxRetryCycles    int // Maximum retry cycles before pausing (0 = unlimited)
}

// QueueMetrics tracks queue statistics
type QueueMetrics struct {
	mu              sync.RWMutex
	enqueueCount    int64
	dequeueCount    int64
	retryCount      int64
	currentDepth    int
	processingItems int
}

// priorityQueue implements heap.Interface
type priorityQueue []*WorkItem

func (pq priorityQueue) Len() int { return len(pq) }

func (pq priorityQueue) Less(i, j int) bool {
	// Higher priority first, then earlier scheduled time
	if pq[i].Priority != pq[j].Priority {
		return pq[i].Priority > pq[j].Priority
	}
	return pq[i].ScheduledAt.Before(pq[j].ScheduledAt)
}

func (pq priorityQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
	pq[i].index = i
	pq[j].index = j
}

func (pq *priorityQueue) Push(x interface{}) {
	n := len(*pq)
	item := x.(*WorkItem)
	item.index = n
	*pq = append(*pq, item)
}

func (pq *priorityQueue) Pop() interface{} {
	old := *pq
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	item.index = -1
	*pq = old[0 : n-1]
	return item
}

// NewWorkQueue creates a new WorkQueue with default retry configuration
func NewWorkQueue() *WorkQueue {
	return NewWorkQueueWithConfig(DefaultMaxRetries, DefaultInitialRetryDelay, DefaultMaxRetryDelay, DefaultMaxRetryCycles)
}

// NewWorkQueueWithConfig creates a new WorkQueue with custom retry configuration
func NewWorkQueueWithConfig(maxRetries int, initialDelay, maxDelay time.Duration, maxCycles int) *WorkQueue {
	wq := &WorkQueue{
		items:             make(priorityQueue, 0),
		itemsMap:          make(map[types.NamespacedName]*WorkItem),
		metrics:           &QueueMetrics{},
		MaxRetries:        maxRetries,
		InitialRetryDelay: initialDelay,
		MaxRetryDelay:     maxDelay,
		MaxRetryCycles:    maxCycles,
	}
	wq.cond = sync.NewCond(&wq.mu)
	heap.Init(&wq.items)
	return wq
}

// Enqueue adds an item to the queue
func (wq *WorkQueue) Enqueue(namespacedName types.NamespacedName, priority int) {
	wq.mu.Lock()
	defer wq.mu.Unlock()

	log := logf.Log.WithName("work-queue")

	// Check if item already exists (deduplication)
	if existingItem, exists := wq.itemsMap[namespacedName]; exists {
		// Update priority if higher
		if priority > existingItem.Priority {
			existingItem.Priority = priority
			heap.Fix(&wq.items, existingItem.index)
			log.V(1).Info("Updated item priority", "item", namespacedName, "priority", priority)
		} else {
			log.V(1).Info("Skipping duplicate enqueue", "item", namespacedName, "existingRetryCount", existingItem.RetryCount)
		}
		return
	}

	// Add new item
	item := &WorkItem{
		NamespacedName: namespacedName,
		Priority:       priority,
		RetryCount:     0,
		EnqueuedAt:     time.Now(),
		ScheduledAt:    time.Now(),
	}

	heap.Push(&wq.items, item)
	wq.itemsMap[namespacedName] = item

	wq.metrics.mu.Lock()
	wq.metrics.enqueueCount++
	wq.metrics.currentDepth = len(wq.items)
	wq.metrics.mu.Unlock()

	log.V(1).Info("Enqueued item", "item", namespacedName, "priority", priority, "queueDepth", len(wq.items))

	wq.cond.Signal()
}

// Dequeue retrieves the next item from the queue, blocking if empty
func (wq *WorkQueue) Dequeue() (*WorkItem, bool) {
	wq.mu.Lock()
	defer wq.mu.Unlock()

	for {
		// Check if shutdown
		if wq.shutdown {
			return nil, false
		}

		// Check if there are items ready to process
		if len(wq.items) > 0 {
			now := time.Now()
			item := wq.items[0]

			// Check if item is ready (for delayed retries)
			if now.Before(item.ScheduledAt) {
				// Calculate wait time
				waitTime := item.ScheduledAt.Sub(now)
				// Wait with timeout
				timer := time.AfterFunc(waitTime, func() {
					wq.cond.Signal()
				})
				wq.cond.Wait()
				timer.Stop()
				continue
			}

			// Remove from heap
			heap.Pop(&wq.items)
			delete(wq.itemsMap, item.NamespacedName)

			wq.metrics.mu.Lock()
			wq.metrics.dequeueCount++
			wq.metrics.currentDepth = len(wq.items)
			wq.metrics.processingItems++
			wq.metrics.mu.Unlock()

			return item, true
		}

		// Wait for new items
		wq.cond.Wait()
	}
}

// Requeue adds an item back to the queue with exponential backoff
func (wq *WorkQueue) Requeue(item *WorkItem, err error) {
	wq.mu.Lock()
	defer wq.mu.Unlock()

	log := logf.Log.WithName("work-queue")

	item.RetryCount++

	wq.metrics.mu.Lock()
	wq.metrics.processingItems--
	wq.metrics.mu.Unlock()

	var delay time.Duration
	if item.RetryCount > wq.MaxRetries {
		// Check if max retry cycles exceeded (0 = unlimited)
		if wq.MaxRetryCycles > 0 && item.RetryCycle >= wq.MaxRetryCycles {
			log.Error(err, "Maximum retry cycles exceeded, giving up",
				"item", item.NamespacedName,
				"cycles", item.RetryCycle,
				"maxCycles", wq.MaxRetryCycles)
			// Don't re-enqueue - template will be marked as Paused by worker
			wq.metrics.mu.Lock()
			wq.metrics.processingItems--
			wq.metrics.mu.Unlock()
			return
		}
		
		// Reset retry count and start a new cycle after cooldown period
		item.RetryCycle++
		item.RetryCount = 0
		delay = wq.MaxRetryDelay
		log.Info("Max retries exceeded, resetting counter after cooldown",
			"item", item.NamespacedName,
			"cycle", item.RetryCycle,
			"cooldown", delay,
			"error", err)
	} else {
		// Calculate exponential backoff
		// Protect against integer overflow by capping retryCount
		retryCount := item.RetryCount - 1
		if retryCount > 30 { // 1<<30 is already huge, cap it to prevent overflow
			retryCount = 30
		}
		delay = wq.InitialRetryDelay * time.Duration(1<<uint(retryCount)) // #nosec G115 -- retryCount is capped at 30 to prevent overflow
		if delay > wq.MaxRetryDelay {
			delay = wq.MaxRetryDelay
		}
	}

	item.ScheduledAt = time.Now().Add(delay)

	heap.Push(&wq.items, item)
	wq.itemsMap[item.NamespacedName] = item

	wq.metrics.mu.Lock()
	wq.metrics.retryCount++
	wq.metrics.currentDepth = len(wq.items)
	wq.metrics.mu.Unlock()

	log.Info("Requeued item with backoff", "item", item.NamespacedName, "retryCount", item.RetryCount, "delay", delay)

	wq.cond.Signal()
}

// Done marks an item as successfully processed
func (wq *WorkQueue) Done(item *WorkItem) {
	wq.metrics.mu.Lock()
	wq.metrics.processingItems--
	wq.metrics.mu.Unlock()
}

// Shutdown gracefully shuts down the queue
func (wq *WorkQueue) Shutdown() {
	wq.mu.Lock()
	defer wq.mu.Unlock()

	wq.shutdown = true
	wq.cond.Broadcast()
}

// GetMetrics returns a snapshot of queue metrics
func (wq *WorkQueue) GetMetrics() QueueMetrics {
	wq.metrics.mu.RLock()
	defer wq.metrics.mu.RUnlock()

	return QueueMetrics{
		enqueueCount:    wq.metrics.enqueueCount,
		dequeueCount:    wq.metrics.dequeueCount,
		retryCount:      wq.metrics.retryCount,
		currentDepth:    wq.metrics.currentDepth,
		processingItems: wq.metrics.processingItems,
	}
}

// Len returns the current queue depth
func (wq *WorkQueue) Len() int {
	wq.mu.Lock()
	defer wq.mu.Unlock()

	return len(wq.items)
}

// Contains checks if an item is currently in the queue
// This is used to prevent duplicate enqueues and status update conflicts
func (wq *WorkQueue) Contains(namespacedName types.NamespacedName) bool {
	wq.mu.Lock()
	defer wq.mu.Unlock()

	_, exists := wq.itemsMap[namespacedName]
	return exists
}
