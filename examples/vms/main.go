// It uses go-proxmox-rest's fakeapi package to seed an in-memory, two-
// cluster Proxmox setup, so it runs standalone — no real Proxmox cluster
// required.
//
// Run it with: go run ./examples/findvms
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	pxpool "github.com/sergelogvinov/go-proxmox-pool"
	pxcluster "github.com/sergelogvinov/go-proxmox-rest/cluster"
	"github.com/sergelogvinov/go-proxmox-rest/fakeapi"
	"github.com/sergelogvinov/go-proxmox-rest/nodes/qemu"
)

// vmCacheTTL is how long Cluster.List/Get's underlying per-cluster VM
// listing is cached before the next call re-fetches it. Both examples
// below share this one pool, and therefore this one TTL.
const vmCacheTTL = 30 * time.Second

func main() {
	cl1, cl2 := seedClusters()
	defer cl1.Close()
	defer cl2.Close()

	pool, err := pxpool.NewProxmoxPool(
		[]*pxpool.ClusterConfig{
			{Region: "cluster-1"},
			{Region: "cluster-2"},
		},
		pxpool.WithCacheTTL(pxpool.ResourceKindVM, vmCacheTTL),
	)
	if err != nil {
		log.Fatalf("building pool: %v", err)
	}

	// In production these clients come from the URL/credentials in
	// ClusterConfig; here they're pointed at the in-memory fakeapi
	// servers instead, exactly the substitution pool_test.go itself
	// uses via ProxmoxPool.Set.
	pool.Set("cluster-1", cl1.Client(nil))
	pool.Set("cluster-2", cl2.Client(nil))

	ctx := context.Background()

	fmt.Println("--- example 1: find by VMID, cluster known ---")
	findByID(ctx, pool, "cluster-1", "101")

	fmt.Println("\n--- example 2: find by SMBIOS UUID, cluster unknown ---")
	findByUUID(ctx, pool, "11111111-1111-1111-1111-111111111111")
}

// seedClusters builds two single-node fake clusters: cluster-1/pve1 with
// two VMs (one carrying an SMBIOS UUID), and cluster-2/pve2 with one VM
// that doesn't match either example's target.
func seedClusters() (cl1, cl2 *fakeapi.Cluster) {
	cl1 = fakeapi.NewCluster(nil, fakeapi.WithNodes("pve1"))
	cl1.Node("pve1").AddVM(100, &qemu.Config{Name: "web-1"})
	cl1.Node("pve1").AddVM(101, &qemu.Config{
		Name:    "web-2",
		SMBios1: &qemu.SMBios1{UUID: "11111111-1111-1111-1111-111111111111"},
	})

	cl2 = fakeapi.NewCluster(nil, fakeapi.WithNodes("pve2"))
	cl2.Node("pve2").AddVM(200, &qemu.Config{Name: "db-1"})

	return cl1, cl2
}

// findByID looks up one VM by VMID within a single, already-known
// cluster via Cluster.Get. It's called twice so the second call's timing
// shows the effect of vmCacheTTL: the underlying cluster.Resources().List
// call only actually reaches the fake server once.
func findByID(ctx context.Context, pool *pxpool.ProxmoxPool, cluster, vmid string) {
	rs, elapsed := timedGet(ctx, pool, cluster, vmid)
	if rs == nil {
		log.Fatalf("VM %s not found in %s", vmid, cluster)
	}

	fmt.Printf("first lookup:  VM %d (%s) on node %s, cluster %s — %s\n", rs.VMID, rs.Name, rs.Node, cluster, elapsed)

	rs, elapsed = timedGet(ctx, pool, cluster, vmid)
	fmt.Printf("second lookup: VM %d (%s) on node %s, cluster %s — %s (served from the %s VM cache)\n",
		rs.VMID, rs.Name, rs.Node, cluster, elapsed, vmCacheTTL)
}

func timedGet(ctx context.Context, pool *pxpool.ProxmoxPool, cluster, vmid string) (*pxcluster.Resource, time.Duration) {
	start := time.Now()

	rs, err := pool.Cluster(cluster).Get(ctx, pxpool.ResourceKindVM, vmid)
	if err != nil {
		if errors.Is(err, pxpool.ErrResourceNotFound) {
			return nil, time.Since(start)
		}

		log.Fatalf("Cluster(%s).Get: %v", cluster, err)
	}

	return rs, time.Since(start)
}

func findByUUID(ctx context.Context, pool *pxpool.ProxmoxPool, uuid string) {
	rs, cluster, elapsed := timedFindByUUID(ctx, pool, uuid)
	if rs == nil {
		log.Fatalf("no VM with UUID %s found in any cluster", uuid)
	}

	fmt.Printf("first lookup:  VM %d (%s) on node %s, cluster %s — %s (resolved via per-VM Config fetch)\n",
		rs.VMID, rs.Name, rs.Node, cluster, elapsed)

	rs, cluster, elapsed = timedFindByUUID(ctx, pool, uuid)
	fmt.Printf("second lookup: VM %d (%s) on node %s, cluster %s — %s (resolved via the pool's UUID index)\n",
		rs.VMID, rs.Name, rs.Node, cluster, elapsed)
}

func timedFindByUUID(ctx context.Context, pool *pxpool.ProxmoxPool, uuid string) (*pxcluster.Resource, string, time.Duration) {
	start := time.Now()

	for _, name := range pool.List() {
		vms, err := pool.Cluster(name).List(ctx, pxpool.ResourceKindVM, pxpool.WithUUID(uuid))
		if err != nil {
			log.Fatalf("Cluster(%s).List: %v", name, err)
		}

		if len(vms) > 0 {
			return &vms[0], name, time.Since(start)
		}
	}

	return nil, "", time.Since(start)
}
