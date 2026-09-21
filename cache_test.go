/*
Copyright 2026 The Kubernetes Authors.

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

package proxmoxpool

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	pxcluster "github.com/sergelogvinov/go-proxmox-rest/cluster"
)

func TestCacheEntryZeroTTLAlwaysRefetches(t *testing.T) {
	var calls atomic.Int32

	e := &cacheEntry{}
	fetch := func() ([]pxcluster.Resource, error) {
		calls.Add(1)

		return []pxcluster.Resource{{VMID: 100}}, nil
	}

	_, err := e.get(0, fetch)
	assert.Nil(t, err)

	_, err = e.get(0, fetch)
	assert.Nil(t, err)

	assert.Equal(t, int32(2), calls.Load())
}

func TestCacheEntryHitWithinTTLSkipsFetch(t *testing.T) {
	var calls atomic.Int32

	e := &cacheEntry{}
	fetch := func() ([]pxcluster.Resource, error) {
		calls.Add(1)

		return []pxcluster.Resource{{VMID: 100}}, nil
	}

	data, err := e.get(time.Minute, fetch)
	assert.Nil(t, err)
	assert.Len(t, data, 1)

	data, err = e.get(time.Minute, fetch)
	assert.Nil(t, err)
	assert.Len(t, data, 1)

	assert.Equal(t, int32(1), calls.Load())
}

func TestCacheEntryConcurrentMissesCollapseIntoOneFetch(t *testing.T) {
	var calls atomic.Int32

	e := &cacheEntry{}
	fetch := func() ([]pxcluster.Resource, error) {
		calls.Add(1)
		time.Sleep(10 * time.Millisecond)

		return []pxcluster.Resource{{VMID: 100}}, nil
	}

	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			_, err := e.get(time.Minute, fetch)
			assert.Nil(t, err)
		})
	}
	wg.Wait()

	assert.Equal(t, int32(1), calls.Load())
}

func TestCacheEntryServesStaleDataOnRefreshError(t *testing.T) {
	e := &cacheEntry{}

	_, err := e.get(time.Millisecond, func() ([]pxcluster.Resource, error) {
		return []pxcluster.Resource{{VMID: 100}}, nil
	})
	assert.Nil(t, err)

	time.Sleep(2 * time.Millisecond)

	data, err := e.get(time.Millisecond, func() ([]pxcluster.Resource, error) {
		return nil, errors.New("boom")
	})
	assert.Nil(t, err)
	assert.Len(t, data, 1)
	assert.Equal(t, 100, data[0].VMID)
}

func TestCacheEntryPropagatesErrorWithNoStaleData(t *testing.T) {
	e := &cacheEntry{}

	data, err := e.get(time.Minute, func() ([]pxcluster.Resource, error) {
		return nil, errors.New("boom")
	})
	assert.NotNil(t, err)
	assert.Nil(t, data)
}

func TestResourceCacheTTLFor(t *testing.T) {
	rc := newResourceCache(map[ResourceKind]time.Duration{
		ResourceKindVM: 10 * time.Second,
	})

	assert.Equal(t, 10*time.Second, rc.ttlFor(ResourceKindVM))
	assert.Equal(t, time.Duration(0), rc.ttlFor(ResourceKindLXC))
}

func TestResourceCacheEntryIsPerKeySingleton(t *testing.T) {
	rc := newResourceCache(nil)

	a := rc.entry(cacheKey{kind: ResourceKindVM, cluster: "cluster-1"})
	b := rc.entry(cacheKey{kind: ResourceKindVM, cluster: "cluster-1"})
	c := rc.entry(cacheKey{kind: ResourceKindVM, cluster: "cluster-2"})

	assert.Same(t, a, b)
	assert.NotSame(t, a, c)
}
