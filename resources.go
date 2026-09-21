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

	proxmoxrest "github.com/sergelogvinov/go-proxmox-rest"
	pxcluster "github.com/sergelogvinov/go-proxmox-rest/cluster"
)

// ResourceKind selects which shape of GET /cluster/resources entry
// Cluster.List/Get fetches.
type ResourceKind int

const (
	// ResourceKindVM matches QEMU virtual machines.
	ResourceKindVM ResourceKind = iota
	// ResourceKindLXC matches LXC containers.
	ResourceKindLXC
	// ResourceKindNode matches cluster nodes.
	ResourceKindNode
	// ResourceKindStorage matches storages.
	ResourceKindStorage
)

// listFilter returns the pxcluster.ListFilter used to populate this kind's
// cache bucket — Type/GuestType only; every other ListOption is applied
// client-side against the cached listing (see Cluster.List).
func (k ResourceKind) listFilter() pxcluster.ListFilter {
	switch k {
	case ResourceKindVM:
		return pxcluster.ListFilter{Type: pxcluster.ResourceTypeVM, GuestType: "qemu"}
	case ResourceKindLXC:
		return pxcluster.ListFilter{Type: pxcluster.ResourceTypeVM, GuestType: "lxc"}
	case ResourceKindNode:
		return pxcluster.ListFilter{Type: pxcluster.ResourceTypeNode}
	case ResourceKindStorage:
		return pxcluster.ListFilter{Type: pxcluster.ResourceTypeStorage}
	default:
		return pxcluster.ListFilter{}
	}
}

// listOptions accumulates every ListOption passed to Cluster.List.
type listOptions struct {
	vmid          int
	storageID     string
	skipTemplates bool
	match         func(*pxcluster.Resource) (bool, error)
	uuid          string
}

// ListOption narrows the entries Cluster.List returns.
type ListOption func(*listOptions)

// WithVMID restricts results to the guest with the given VMID. Meaningful
// for ResourceKindVM/ResourceKindLXC only; ignored against other kinds.
func WithVMID(vmid int) ListOption {
	return func(o *listOptions) { o.vmid = vmid }
}

// WithStorageID restricts results to the given storage ID. Meaningful for
// ResourceKindStorage only; ignored against other kinds.
func WithStorageID(id string) ListOption {
	return func(o *listOptions) { o.storageID = id }
}

// SkipTemplates excludes guests flagged as templates.
func SkipTemplates() ListOption {
	return func(o *listOptions) { o.skipTemplates = true }
}

// WithMatch keeps only entries for which fn returns true. fn is evaluated
// against whatever Cluster.List's cache (or a live fetch) returned; an
// error return aborts List and is returned to the caller.
func WithMatch(fn func(*pxcluster.Resource) (bool, error)) ListOption {
	return func(o *listOptions) { o.match = fn }
}

// WithUUID restricts ResourceKindVM entries to the single guest whose
// SMBIOS UUID matches uuid, resolved via the pool's UUID index (see
// resolveByUUID). Ignored against every other ResourceKind — LXC
// containers have no equivalent identity field.
func WithUUID(uuid string) ListOption {
	return func(o *listOptions) { o.uuid = uuid }
}

// uuidIndexEntry is what a successful UUID resolution remembers — just
// enough to skip straight to the right VM next time.
type uuidIndexEntry struct {
	cluster string
	vmID    int
}

// uuidLookup returns the pool's remembered resolution for uuid, if any.
func (p *ProxmoxPool) uuidLookup(uuid string) (uuidIndexEntry, bool) {
	v, ok := p.uuidIndex.Load(uuid)
	if !ok {
		return uuidIndexEntry{}, false
	}

	entry, ok := v.(uuidIndexEntry)

	return entry, ok
}

// uuidStore remembers that uuid resolves to entry.
func (p *ProxmoxPool) uuidStore(uuid string, entry uuidIndexEntry) {
	p.uuidIndex.Store(uuid, entry)
}

// uuidEvict forgets a stale resolution for uuid (its VM no longer appears
// in the cluster it pointed to).
func (p *ProxmoxPool) uuidEvict(uuid string) {
	p.uuidIndex.Delete(uuid)
}

// resolveByUUID narrows candidates (already filtered by every other
// ListOption in this call) to the single guest whose SMBIOS UUID matches
// uuid, consulting/populating the pool's UUID index. all is this cluster's
// full, unfiltered ResourceKindVM listing (from the same cache read List
// used), needed to confirm an index hit's VM hasn't since been deleted.
//
// Returns an empty, nil-error slice when no guest matches — the same
// not-found shape List's other filters produce, so callers can treat a
// UUID miss identically to any other empty List result.
func resolveByUUID(ctx context.Context, c *Cluster, px *proxmoxrest.Client, uuid string, all, candidates []pxcluster.Resource) ([]pxcluster.Resource, error) {
	if entry, ok := c.pool.uuidLookup(uuid); ok {
		if entry.cluster != c.name {
			return nil, nil
		}

		if vmidPresent(all, entry.vmID) {
			for i := range candidates {
				if candidates[i].VMID == entry.vmID {
					return candidates[i : i+1 : i+1], nil
				}
			}

			return nil, nil
		}

		c.pool.uuidEvict(uuid)
	}

	for i := range candidates {
		rs := &candidates[i]

		if rs.Status == "unknown" {
			continue
		}

		cfg, err := px.Nodes(rs.Node).Qemu().Config(ctx, rs.VMID, nil)
		if err != nil {
			return nil, err
		}

		if cfg.SMBios1 != nil && cfg.SMBios1.UUID == uuid {
			c.pool.uuidStore(uuid, uuidIndexEntry{cluster: c.name, vmID: rs.VMID})

			return candidates[i : i+1 : i+1], nil
		}
	}

	return nil, nil
}

func vmidPresent(resources []pxcluster.Resource, vmid int) bool {
	for i := range resources {
		if resources[i].VMID == vmid {
			return true
		}
	}

	return false
}
