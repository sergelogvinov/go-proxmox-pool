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
	"context"
	"fmt"
	"strconv"

	proxmoxrest "github.com/sergelogvinov/go-proxmox-rest"
	pxcluster "github.com/sergelogvinov/go-proxmox-rest/cluster"
)

// Cluster is a pool-bound handle scoped to one Proxmox cluster. It is cheap
// to create — Cluster(name) does no I/O — and holds no state of its own;
// the pool owns the underlying REST client and cache, so cache entries
// persist across separately-obtained Cluster values for the same name.
type Cluster struct {
	pool *ProxmoxPool
	name string
}

// client resolves the handle's underlying REST client, surfacing
// ErrClusterNotFound if the cluster was never configured.
func (c *Cluster) client() (*proxmoxrest.Client, error) {
	return c.pool.Get(c.name)
}

// Check probes the cluster's connectivity and permissions: it fetches the
// Proxmox version and lists VM resources, the same check today's
// CheckClusters performed per cluster.
func (c *Cluster) Check(ctx context.Context) error {
	px, err := c.client()
	if err != nil {
		return err
	}

	if _, err := px.Version(ctx); err != nil {
		return fmt.Errorf("cluster %s: failed to get version: %w", c.name, err)
	}

	if _, err := px.Cluster().Resources().List(ctx, pxcluster.ListFilter{Type: pxcluster.ResourceTypeVM}); err != nil {
		return fmt.Errorf("cluster %s: failed to list VMs: %w", c.name, err)
	}

	return nil
}

// List returns this cluster's kind-scoped resource listing, narrowed by
// opts. The underlying (kind, cluster) listing is cached per the pool's
// WithCacheTTL configuration for kind; opts are applied client-side against
// whatever the cache (or a live fetch, if caching is disabled) returned.
func (c *Cluster) List(ctx context.Context, kind ResourceKind, opts ...ListOption) ([]pxcluster.Resource, error) {
	px, err := c.client()
	if err != nil {
		return nil, err
	}

	var o listOptions
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}

	filter := kind.listFilter()

	all, err := c.pool.cache.entry(cacheKey{kind: kind, cluster: c.name}).get(
		c.pool.cache.ttlFor(kind),
		func() ([]pxcluster.Resource, error) {
			return px.Cluster().Resources().List(ctx, filter)
		},
	)
	if err != nil {
		return nil, err
	}

	candidates := make([]pxcluster.Resource, 0, len(all))

	for i := range all {
		rs := &all[i]

		if o.vmid != 0 && rs.VMID != o.vmid {
			continue
		}

		if o.storageID != "" && rs.Storage != o.storageID {
			continue
		}

		if o.skipTemplates && rs.Template == 1 {
			continue
		}

		if o.match != nil {
			ok, err := o.match(rs)
			if err != nil {
				return nil, err
			}

			if !ok {
				continue
			}
		}

		candidates = append(candidates, *rs)
	}

	if o.uuid != "" && kind == ResourceKindVM {
		return resolveByUUID(ctx, c, px, o.uuid, all, candidates)
	}

	return candidates, nil
}

// Get returns the single resource identified by id within kind: id is the
// VMID for ResourceKindVM/ResourceKindLXC, or the storage ID for
// ResourceKindStorage — whichever scalar identifier that kind's entries are
// addressed by. ResourceKindNode has none Get understands; calling Get with
// it (or passing a VMID that doesn't parse as an integer) is a documented
// no-op returning ErrResourceNotFound.
func (c *Cluster) Get(ctx context.Context, kind ResourceKind, id string) (*pxcluster.Resource, error) {
	var opts []ListOption

	switch kind {
	case ResourceKindVM, ResourceKindLXC:
		vmid, err := strconv.Atoi(id)
		if err != nil {
			return nil, ErrResourceNotFound
		}

		opts = []ListOption{WithVMID(vmid), SkipTemplates()}
	case ResourceKindStorage:
		opts = []ListOption{WithStorageID(id)}
	default:
		return nil, ErrResourceNotFound
	}

	resources, err := c.List(ctx, kind, opts...)
	if err != nil {
		return nil, err
	}

	if len(resources) == 0 {
		return nil, ErrResourceNotFound
	}

	return &resources[0], nil
}
