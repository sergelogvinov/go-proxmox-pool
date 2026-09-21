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

package proxmoxpool_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	pxpool "github.com/sergelogvinov/go-proxmox-pool"
	pxcluster "github.com/sergelogvinov/go-proxmox-rest/cluster"
	"github.com/sergelogvinov/go-proxmox-rest/fakeapi"
	"github.com/sergelogvinov/go-proxmox-rest/nodes/lxc"
	"github.com/sergelogvinov/go-proxmox-rest/nodes/qemu"
)

// newFakePool builds a single-cluster pool ("cluster-1") backed by cl.
func newFakePool(t *testing.T, cl *fakeapi.Cluster, options ...pxpool.Option) *pxpool.ProxmoxPool {
	t.Helper()

	pool, err := pxpool.NewProxmoxPool([]*pxpool.ClusterConfig{{ClusterName: "cluster-1"}}, options...)
	assert.Nil(t, err)

	pool.Set("cluster-1", cl.Client(t))

	return pool
}

func TestClusterListMapsResourceKindToFilter(t *testing.T) {
	cl := fakeapi.NewCluster(t, fakeapi.WithNodes("pve1"))
	cl.Node("pve1").AddVM(100, &qemu.Config{Name: "vm-100"})
	cl.Node("pve1").AddContainer(200, &lxc.Config{Hostname: "ct-200"})
	cl.Node("pve1").AddStorage("local", "dir")

	pool := newFakePool(t, cl)
	c := pool.Cluster("cluster-1")

	vms, err := c.List(t.Context(), pxpool.ResourceKindVM)
	assert.Nil(t, err)
	assert.Len(t, vms, 1)
	assert.Equal(t, 100, vms[0].VMID)

	lxcs, err := c.List(t.Context(), pxpool.ResourceKindLXC)
	assert.Nil(t, err)
	assert.Len(t, lxcs, 1)
	assert.Equal(t, 200, lxcs[0].VMID)

	nodes, err := c.List(t.Context(), pxpool.ResourceKindNode)
	assert.Nil(t, err)
	assert.Len(t, nodes, 1)

	storages, err := c.List(t.Context(), pxpool.ResourceKindStorage)
	assert.Nil(t, err)
	assert.Len(t, storages, 1)
	assert.Equal(t, "local", storages[0].Storage)
}

func TestClusterListFiltersClientSide(t *testing.T) {
	cl := fakeapi.NewCluster(t, fakeapi.WithNodes("pve1"))
	cl.Node("pve1").AddVM(100, &qemu.Config{Name: "web-1"})
	cl.Node("pve1").AddVM(101, &qemu.Config{Name: "web-2"})

	pool := newFakePool(t, cl)
	c := pool.Cluster("cluster-1")

	vms, err := c.List(t.Context(), pxpool.ResourceKindVM, pxpool.WithVMID(101))
	assert.Nil(t, err)
	assert.Len(t, vms, 1)
	assert.Equal(t, 101, vms[0].VMID)

	vms, err = c.List(t.Context(), pxpool.ResourceKindVM, pxpool.WithMatch(func(rs *pxcluster.Resource) (bool, error) {
		return strings.HasPrefix(rs.Name, "web-2"), nil
	}))
	assert.Nil(t, err)
	assert.Len(t, vms, 1)
	assert.Equal(t, 101, vms[0].VMID)
}

func TestClusterListMissingClusterSurfacesErrOnFirstUse(t *testing.T) {
	pool, err := pxpool.NewProxmoxPool([]*pxpool.ClusterConfig{{ClusterName: "cluster-1"}})
	assert.Nil(t, err)

	c := pool.Cluster("missing")

	_, err = c.List(t.Context(), pxpool.ResourceKindVM)
	assert.Equal(t, pxpool.ErrClusterNotFound, err)
}

func TestClusterGetReturnsResourceNotFound(t *testing.T) {
	cl := fakeapi.NewCluster(t, fakeapi.WithNodes("pve1"))

	pool := newFakePool(t, cl)
	c := pool.Cluster("cluster-1")

	_, err := c.Get(t.Context(), pxpool.ResourceKindVM, "999")
	assert.Equal(t, pxpool.ErrResourceNotFound, err)
}

func TestClusterGetStorageByID(t *testing.T) {
	cl := fakeapi.NewCluster(t, fakeapi.WithNodes("pve1"))
	cl.Node("pve1").AddStorage("local", "dir")
	cl.Node("pve1").AddStorage("ceph", "rbd")

	pool := newFakePool(t, cl)
	c := pool.Cluster("cluster-1")

	rs, err := c.Get(t.Context(), pxpool.ResourceKindStorage, "ceph")
	assert.Nil(t, err)
	assert.Equal(t, "ceph", rs.Storage)

	_, err = c.Get(t.Context(), pxpool.ResourceKindStorage, "missing")
	assert.Equal(t, pxpool.ErrResourceNotFound, err)
}

func TestClusterGetVMWithMalformedIDIsResourceNotFound(t *testing.T) {
	cl := fakeapi.NewCluster(t, fakeapi.WithNodes("pve1"))

	pool := newFakePool(t, cl)
	c := pool.Cluster("cluster-1")

	_, err := c.Get(t.Context(), pxpool.ResourceKindVM, "not-a-number")
	assert.Equal(t, pxpool.ErrResourceNotFound, err)
}

func TestClusterGetNodeKindIsUnsupported(t *testing.T) {
	cl := fakeapi.NewCluster(t, fakeapi.WithNodes("pve1"))

	pool := newFakePool(t, cl)
	c := pool.Cluster("cluster-1")

	_, err := c.Get(t.Context(), pxpool.ResourceKindNode, "pve1")
	assert.Equal(t, pxpool.ErrResourceNotFound, err)
}

func TestClusterListCachesWithinTTL(t *testing.T) {
	cl := fakeapi.NewCluster(t, fakeapi.WithNodes("pve1"))
	cl.Node("pve1").AddVM(100, &qemu.Config{Name: "vm-100"})

	pool := newFakePool(t, cl, pxpool.WithCacheTTL(pxpool.ResourceKindVM, time.Minute))
	c := pool.Cluster("cluster-1")

	vms, err := c.List(t.Context(), pxpool.ResourceKindVM)
	assert.Nil(t, err)
	assert.Len(t, vms, 1)

	// Add a second VM directly against the fake state; a cached List
	// call must not observe it within the TTL.
	cl.Node("pve1").AddVM(101, &qemu.Config{Name: "vm-101"})

	vms, err = c.List(t.Context(), pxpool.ResourceKindVM)
	assert.Nil(t, err)
	assert.Len(t, vms, 1, "expected cached listing to still report one VM within the TTL")
}

func TestClusterListWithUUID(t *testing.T) {
	cl := fakeapi.NewCluster(t, fakeapi.WithNodes("pve1"))
	cl.Node("pve1").AddVM(100, &qemu.Config{Name: "vm-100", SMBios1: &qemu.SMBios1{UUID: "uuid-a"}})
	cl.Node("pve1").AddVM(101, &qemu.Config{Name: "vm-101", SMBios1: &qemu.SMBios1{UUID: "uuid-b"}})

	pool := newFakePool(t, cl)
	c := pool.Cluster("cluster-1")

	vms, err := c.List(t.Context(), pxpool.ResourceKindVM, pxpool.WithUUID("uuid-b"))
	assert.Nil(t, err)
	assert.Len(t, vms, 1)
	assert.Equal(t, 101, vms[0].VMID)

	// Second lookup should hit the warm index (no error, same result) —
	// behavior is externally identical; the index's effect is on request
	// volume, verified separately against the fake's task/request surface
	// is out of scope here.
	vms, err = c.List(t.Context(), pxpool.ResourceKindVM, pxpool.WithUUID("uuid-b"))
	assert.Nil(t, err)
	assert.Len(t, vms, 1)
	assert.Equal(t, 101, vms[0].VMID)

	vms, err = c.List(t.Context(), pxpool.ResourceKindVM, pxpool.WithUUID("uuid-missing"))
	assert.Nil(t, err)
	assert.Len(t, vms, 0)
}

func TestClusterListWithUUIDWrongClusterShortCircuits(t *testing.T) {
	cl1 := fakeapi.NewCluster(t, fakeapi.WithNodes("pve1"))
	cl1.Node("pve1").AddVM(100, &qemu.Config{Name: "vm-100", SMBios1: &qemu.SMBios1{UUID: "uuid-a"}})

	cl2 := fakeapi.NewCluster(t, fakeapi.WithNodes("pve2"))

	pool, err := pxpool.NewProxmoxPool([]*pxpool.ClusterConfig{
		{ClusterName: "cluster-1"},
		{ClusterName: "cluster-2"},
	})
	assert.Nil(t, err)
	pool.Set("cluster-1", cl1.Client(t))
	pool.Set("cluster-2", cl2.Client(t))

	vms, err := pool.Cluster("cluster-1").List(t.Context(), pxpool.ResourceKindVM, pxpool.WithUUID("uuid-a"))
	assert.Nil(t, err)
	assert.Len(t, vms, 1)

	// Now that uuid-a is indexed against cluster-1, cluster-2 must return
	// empty without erroring (no VM there at all, so a naive scan would
	// also find nothing — the point is this returns fast via the index).
	vms, err = pool.Cluster("cluster-2").List(t.Context(), pxpool.ResourceKindVM, pxpool.WithUUID("uuid-a"))
	assert.Nil(t, err)
	assert.Len(t, vms, 0)
}

func TestClusterGetVMConfig(t *testing.T) {
	cl := fakeapi.NewCluster(t, fakeapi.WithNodes("pve1"))
	cl.Node("pve1").AddVM(100, &qemu.Config{Name: "vm-100", SMBios1: &qemu.SMBios1{UUID: "uuid-a"}})

	pool := newFakePool(t, cl)
	c := pool.Cluster("cluster-1")

	details, err := c.GetVMConfig(t.Context(), 100)
	assert.Nil(t, err)
	assert.Equal(t, 100, details.VMID)
	assert.Equal(t, "pve1", details.Node)
	assert.Equal(t, "uuid-a", details.UUID)

	// GetVMConfig must have opportunistically warmed the UUID index.
	vms, err := c.List(t.Context(), pxpool.ResourceKindVM, pxpool.WithUUID("uuid-a"))
	assert.Nil(t, err)
	assert.Len(t, vms, 1)
	assert.Equal(t, 100, vms[0].VMID)
}

func TestClusterGetNodeHAGroups(t *testing.T) {
	cl := fakeapi.NewCluster(t, fakeapi.WithNodes("pve1", "pve2"),
		fakeapi.WithHAGroup("group-a", "pve1:1"))

	pool := newFakePool(t, cl)
	c := pool.Cluster("cluster-1")

	groups, err := c.GetNodeHAGroups(t.Context(), "pve1")
	assert.Nil(t, err)
	assert.Equal(t, []string{"group-a"}, groups)

	_, err = c.GetNodeHAGroups(t.Context(), "pve2")
	assert.Equal(t, pxpool.ErrHAGroupNotFound, err)
}

// Cluster.Check's success path (px.Version + a resources list against a
// reachable cluster) isn't covered here: fakeapi does not implement
// GET /version yet. Its failure path (an unreachable/misconfigured
// cluster) is covered by TestClusterCheck in pool_test.go.
