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
	"sync"
	"time"

	pxcluster "github.com/sergelogvinov/go-proxmox-rest/cluster"
)

// cacheKey identifies one cache bucket: a resource kind within one cluster.
type cacheKey struct {
	kind    ResourceKind
	cluster string
}

// cacheEntry holds one (kind, cluster) bucket's last-known-good listing.
type cacheEntry struct {
	mu        sync.Mutex // held across a refresh; serializes concurrent misses
	data      []pxcluster.Resource
	expiresAt time.Time
}

// get returns data if unexpired; otherwise it calls fetch, holding the
// entry's own mutex so concurrent callers for the same (kind, cluster)
// block behind one in-flight refresh instead of issuing N redundant
// requests. A TTL of zero means "never cached" — get always refreshes.
func (e *cacheEntry) get(ttl time.Duration, fetch func() ([]pxcluster.Resource, error)) ([]pxcluster.Resource, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if ttl > 0 && time.Now().Before(e.expiresAt) {
		return e.data, nil
	}

	data, err := fetch()
	if err != nil {
		// Serve stale data on a refresh error rather than propagating it,
		// as long as we have something cached.
		if e.data != nil {
			return e.data, nil
		}

		return nil, err
	}

	e.data, e.expiresAt = data, time.Now().Add(ttl)

	return data, nil
}

// resourceCache holds every (kind, cluster) cache bucket for a pool, plus
// the per-kind TTL configuration every bucket is refreshed against.
type resourceCache struct {
	entries sync.Map // cacheKey -> *cacheEntry
	ttl     map[ResourceKind]time.Duration
}

// newResourceCache builds a resourceCache from the per-kind TTLs collected
// from NewProxmoxPool's WithCacheTTL options. A nil/empty ttl means every
// kind defaults to 0 (no caching).
func newResourceCache(ttl map[ResourceKind]time.Duration) *resourceCache {
	return &resourceCache{ttl: ttl}
}

// entry returns key's cache bucket, creating it lazily on first access.
func (rc *resourceCache) entry(key cacheKey) *cacheEntry {
	v, _ := rc.entries.LoadOrStore(key, &cacheEntry{})

	return v.(*cacheEntry) //nolint:forcetypeassert
}

// ttlFor returns the configured TTL for kind (zero if never configured).
func (rc *resourceCache) ttlFor(kind ResourceKind) time.Duration {
	return rc.ttl[kind]
}
