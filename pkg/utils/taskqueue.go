/*
Copyright 2017 The Kubernetes Authors.

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

package utils

import (
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"
)

var (
	KeyFunc = cache.DeletionHandlingMetaNamespaceKeyFunc
)

// TaskQueue is a rate limited operation queue.
type TaskQueue interface {
	Run()
	Enqueue(objs ...interface{})
	Shutdown()
}

// PeriodicTaskQueue invokes the given sync function for every work item
// inserted. If the sync() function results in an error, the item is put on
// the work queue after a rate-limit.
type PeriodicTaskQueue struct {
	// resource is used for logging to distinguish the queue being used.
	resource string
	// keyFunc translates an object to a string-based key.
	keyFunc func(obj interface{}) (string, error)
	// queue is the work queue the worker polls.
	queue workqueue.RateLimitingInterface
	// sync is called for each item in the queue.
	sync func(string) error
	// workerDone is closed when the worker exits.
	workerDone chan struct{}
}

// Run the task queue. This will block until the Shutdown() has been called.
func (t *PeriodicTaskQueue) Run() {
	for {
		key, quit := t.queue.Get()
		if quit {
			close(t.workerDone)
			return
		}
		klog.V(4).Infof("Syncing %v (%v)", key, t.resource)
		if err := t.sync(key.(string)); err != nil {
			klog.Errorf("Requeuing %q due to error: %v (%v)", key, err, t.resource)
			t.queue.AddRateLimited(key)
		} else {
			klog.V(4).Infof("Finished syncing %v", key)
			t.queue.Forget(key)
		}
		t.queue.Done(key)
	}
}

// Enqueue one or more keys to the work queue.
func (t *PeriodicTaskQueue) Enqueue(objs ...interface{}) {
	for _, obj := range objs {
		key, err := t.keyFunc(obj)
		if err != nil {
			klog.Errorf("Couldn't get key for object %+v (type %T): %v", obj, obj, err)
			return
		}
		klog.V(4).Infof("Enqueue key=%q (%v)", key, t.resource)
		t.queue.Add(key)
	}
}

// Shutdown shuts down the work queue and waits for the worker to ACK
func (t *PeriodicTaskQueue) Shutdown() {
	klog.V(2).Infof("Shutdown")
	t.queue.ShutDown()
	<-t.workerDone
}

// NewPeriodicTaskQueue creates a new task queue with the default rate limiter.
func NewPeriodicTaskQueue(name, resource string, syncFn func(string) error) *PeriodicTaskQueue {
	rl := workqueue.DefaultControllerRateLimiter()
	return NewPeriodicTaskQueueWithLimiter(name, resource, syncFn, rl)
}

// NewPeriodicTaskQueueWithLimiter creates a new task queue with the given sync function
// and rate limiter. The sync function is called for every element inserted into the queue.
func NewPeriodicTaskQueueWithLimiter(name, resource string, syncFn func(string) error, rl workqueue.RateLimiter) *PeriodicTaskQueue {
	var queue workqueue.RateLimitingInterface
	if name == "" {
		queue = workqueue.NewRateLimitingQueue(rl)
	} else {
		queue = workqueue.NewNamedRateLimitingQueue(rl, name)
	}

	return &PeriodicTaskQueue{
		resource:   resource,
		keyFunc:    KeyFunc,
		queue:      queue,
		sync:       syncFn,
		workerDone: make(chan struct{}),
	}
}
