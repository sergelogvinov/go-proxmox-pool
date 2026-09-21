# go-proxmox-pool — Architecture

`go-proxmox-pool` is a multi-cluster client pool built on top of
[`go-proxmox-rest`](https://github.com/sergelogvinov/go-proxmox-rest). It
holds one REST client per configured Proxmox cluster, exposes a fluent
per-cluster handle for listing/finding VMs, LXCs, nodes, and storages, and
caches that cluster-wide inventory with a configurable, per-resource-kind
TTL instead of re-listing it on every call.

```go
pool.List()                                   // []string — configured cluster names
pool.Get("cluster-1")                         // *proxmoxrest.Client — raw client escape hatch
pool.Cluster("cluster-1").Check(ctx)           // liveness probe, one cluster
pool.Cluster("cluster-1").List(ctx, pxpool.ResourceKindVM)         // cached cluster.Resources() listing
pool.Cluster("cluster-1").Get(ctx, pxpool.ResourceKindVM, "100")   // cached VM lookup by VMID
pool.Cluster("cluster-1").List(ctx, pxpool.ResourceKindVM, pxpool.WithUUID(uuid)) // index-backed UUID lookup
// cross-cluster search: loop pool.List() and call Cluster(name).List per cluster — no dedicated Find* method
```

---

## 1. Package layout

```
go-proxmox-pool
├── go.mod
├── doc.go
├── pool.go        // ClusterConfig, ProxmoxPool, NewProxmoxPool, List, Get, Set, Cluster
├── cluster.go      // the Cluster handle: Check, List, Get
├── options.go      // Option, WithCacheTTL, WithRESTOption
├── cache.go        // the generic TTL cache, per-(kind, cluster) dedup
├── resources.go    // ResourceKind, ListOption, the UUID index
├── vms.go          // GetVMConfig, GetNextID, DeleteVM
├── ha.go           // GetNodeHAGroups
├── errors.go       // consolidated Err* sentinels
├── types.go        // VMDetails
└── *_test.go
```

One flat package: `go-proxmox-pool` is a thin orchestration layer over one
client library, not a wrapper around many REST resources, so a
sub-package tree isn't warranted. `Cluster` lives in the same package as
`ProxmoxPool` since it holds a private `*ProxmoxPool` back-reference and
would otherwise need an import cycle to live elsewhere.

---

## 2. Core types

### `ClusterConfig` and `ProxmoxPool`

```go
type ClusterConfig struct {
	Name            string `yaml:"name,omitempty"`   // display label only, never a lookup key
	ClusterName     string `yaml:"region,omitempty"` // the cluster selector
	URL             string
	Insecure        bool
	TokenID         string
	TokenIDFile     string
	TokenSecret     string
	TokenSecretFile string
	Username        string
	Password        string
}

type ProxmoxPool struct {
	clients   map[string]*proxmoxrest.Client
	cache     *resourceCache
	uuidIndex sync.Map
}

func NewProxmoxPool(config []*ClusterConfig, options ...Option) (*ProxmoxPool, error)
```

`ClusterConfig.ClusterName` is the selector: `pool.List()`, `pool.Get()`,
and `pool.Cluster()` all key on this value. Its YAML tag stays `region`
for config-file compatibility; `ClusterName` is a Go-identifier-only
rename. `Name` is a second, independent field for a free-text display
label — metadata only, never used as a lookup key.

`ProxmoxPool` owns three things: the per-cluster REST clients, the shared
resource cache, and the shared UUID index (§4). It exposes:

```go
func (p *ProxmoxPool) List() []string                                   // configured cluster names
func (p *ProxmoxPool) Get(cluster string) (*proxmoxrest.Client, error)  // raw client escape hatch
func (p *ProxmoxPool) Set(cluster string, client *proxmoxrest.Client)   // override/inject a client (tests)
func (p *ProxmoxPool) Cluster(cluster string) *Cluster                  // scoped handle, no I/O
```

There is no pool-level `Check`. A caller that wants every cluster probed
loops it itself:

```go
for _, name := range pool.List() {
	if err := pool.Cluster(name).Check(ctx); err != nil {
		// handle/aggregate per-cluster
	}
}
```

### The `Cluster` handle

```go
type Cluster struct {
	pool *ProxmoxPool
	name string
}

func (p *ProxmoxPool) Cluster(cluster string) *Cluster {
	return &Cluster{pool: p, name: cluster}
}
```

`Cluster(name)` is cheap and side-effect-free — no client-side existence
check, no I/O — matching `go-proxmox-rest`'s own `client.Nodes(node)`
posture. `ErrClusterNotFound` surfaces from the handle's first actual call
(`Check`, `List`, `Get`, ...), not from `Cluster` itself. Every method that
would otherwise take `(ctx, cluster string, ...)` is a method on `*Cluster`
instead, so the selector is threaded through a receiver once rather than
repeated as an argument everywhere:

```go
func (c *Cluster) Check(ctx context.Context) error
func (c *Cluster) List(ctx context.Context, kind ResourceKind, opts ...ListOption) ([]pxcluster.Resource, error)
func (c *Cluster) Get(ctx context.Context, kind ResourceKind, id string) (*pxcluster.Resource, error)
```

Cache entries and the UUID index live on `*ProxmoxPool`, not `*Cluster`,
so they persist across separately-obtained `Cluster` values for the same
name — a new `Cluster` handle is cheap to create per call site precisely
because it carries no state of its own.

---

## 3. Resource kinds and `Cluster.List`/`Get`

There is one `List`, parameterized by `ResourceKind`:

```go
type ResourceKind int

const (
	ResourceKindVM ResourceKind = iota // -> ListFilter{Type: "vm", GuestType: "qemu"}
	ResourceKindLXC                    // -> ListFilter{Type: "vm", GuestType: "lxc"}
	ResourceKindNode                   // -> ListFilter{Type: "node"}
	ResourceKindStorage                // -> ListFilter{Type: "storage"}
)
```

VM and LXC stay separate kinds (rather than one "guest" kind with a
guest-type filter) because they're cached separately — a caller may want a
shorter TTL on VMs than containers, or cache one but not the other.

`ListOption`s narrow what `List` returns, applied client-side against
whatever the cache (or a live fetch) returned:

```go
func WithVMID(vmid int) ListOption
func WithStorageID(id string) ListOption
func SkipTemplates() ListOption
func WithMatch(fn func(*pxcluster.Resource) (bool, error)) ListOption
func WithUUID(uuid string) ListOption // ResourceKindVM only — see §5
```

`Get` is the single-item convenience, also parameterized by kind rather
than fragmented into `GetVM`/`GetStorage`/etc.:

```go
func (c *Cluster) Get(ctx context.Context, kind ResourceKind, id string) (*pxcluster.Resource, error)
```

`id` is whichever scalar identifier that kind's entries are addressed
by: a VMID (parsed from the string) for `ResourceKindVM`/
`ResourceKindLXC`, a storage ID for `ResourceKindStorage`. `ResourceKindNode`
has no scalar identifier `Get` understands, and a VM/LXC `id` that doesn't
parse as an integer is likewise a documented no-op — both return
`ErrResourceNotFound`, the sentinel for "no resource matched, any kind"
(distinct from `ErrInstanceNotFound`, used by domain methods that are
unambiguously about one VM/LXC guest — see §6).

There is no cross-cluster search method. The pool indexes clients and
cache entries by cluster; it does not merge, summarize, or search across
clusters itself. A caller that wants that loops `pool.List()`:

```go
for _, name := range pool.List() {
	vms, err := pool.Cluster(name).List(ctx, pxpool.ResourceKindVM,
		pxpool.WithMatch(func(rs *pxcluster.Resource) (bool, error) {
			return strings.HasPrefix(rs.Name, namePrefix), nil
		}),
		pxpool.WithUUID(systemUUID),
	)
	if err != nil {
		continue
	}
	if len(vms) > 0 {
		return vms[0].VMID, name, nil
	}
}
```

`ListOption`s compose as a narrowing sequence: cheap, in-memory filters
(`WithMatch`, `WithVMID`, ...) are applied before `WithUUID`'s expensive,
`Config`-fetching resolution, so only already-narrowed candidates ever pay
for a UUID confirmation.

---

## 4. Caching model

### The per-`(kind, cluster)` TTL cache

```go
type cacheEntry struct {
	mu        sync.Mutex
	data      []pxcluster.Resource
	expiresAt time.Time
}

func (e *cacheEntry) get(ttl time.Duration, fetch func() ([]pxcluster.Resource, error)) ([]pxcluster.Resource, error)
```

One `cacheEntry` per `(kind, cluster)` pair, created lazily on a
`sync.Map`. `get` returns the cached data if unexpired; otherwise it calls
`fetch`, holding the entry's own mutex so concurrent callers for the same
`(kind, cluster)` collapse into one in-flight refresh instead of issuing N
redundant requests. Per-entry locking (rather than one pool-wide lock)
means a refresh for one `(kind, cluster)` never blocks a concurrent read
for another.

A fetch error is swallowed in favor of the last-known-good data, as long
as there is any — a refresh failure doesn't turn a working cache cold. TTL
`0` (the default — `WithCacheTTL` never called for that kind) means no
caching at all: every `List` call goes straight to the API.

```go
pool, err := pxpool.NewProxmoxPool(clusters,
	pxpool.WithCacheTTL(pxpool.ResourceKindVM, 10*time.Second),
	pxpool.WithCacheTTL(pxpool.ResourceKindLXC, 10*time.Second),
	pxpool.WithCacheTTL(pxpool.ResourceKindNode, 30*time.Second),
	pxpool.WithCacheTTL(pxpool.ResourceKindStorage, time.Minute),
)
```

TTL applies uniformly across every cluster in the pool for a given kind —
one bucket per `(kind, cluster)`, all sharing that kind's configured TTL.
Negative TTL is rejected at construction. The cache always holds the
**unfiltered** per-`(kind, cluster)` listing; `ListOption`s are applied
client-side to whatever it returns, so two call sites with different
filters share one cache entry instead of each maintaining a narrower,
more-often-missing cache of their own.

### The UUID index

`WithUUID` resolves a guest by its SMBIOS UUID — a value that isn't part of
the cluster-resources listing at all, so resolving it costs one
`Nodes(node).Qemu().Config(ctx, vmid)` call per candidate the first time.
Caching the *listing* faster doesn't make that *per-VM config fetch* any
cheaper, so there's a second, purpose-built cache: a pool-level index from
UUID to the `(cluster, vmID)` that answered it last time.

```go
type uuidIndexEntry struct {
	cluster string
	vmID    int
}
```

This index has no TTL of its own. Correctness instead rides on presence in
the already-TTL-cached VM listing: before trusting an index hit, the
resolving code confirms the `vmID` is still present in that cluster's
current VM listing; if it isn't — the VM was deleted — the stale entry is
evicted and resolution falls back to the slow, per-candidate scan. This
ties the index's staleness bound to the listing cache's own TTL instead of
introducing a second, independently-configured expiry.

The lookup, entirely inside `List(ctx, ResourceKindVM, WithUUID(uuid))`:

1. Check the index. If it names a **different** cluster than the one
   `List` is running against, return empty immediately — no scan, no
   `Config` calls. If it names *this* cluster, confirm the `vmID` is still
   present; a present hit returns with no `Config` call at all, an absent
   one evicts the entry and falls through to step 2.
2. Miss (or evicted stale hit): scan this cluster's VM listing, skipping
   any candidate whose `Status` is `"unknown"`, fetching `Config` for the
   rest and comparing `SMBios1.UUID`. On a match, store the index entry
   before returning.

Because step 1's "wrong cluster" case is an O(1) map lookup, a caller
looping every cluster looking for one UUID doesn't pay for a full scan in
each cluster that turns out to be the wrong one — only the one correct
cluster ever reaches step 2's expensive path, and only on that UUID's
first resolution.

The index is also populated **opportunistically** by any other path that
happens to fetch a VM's `Config` for its own reasons — `GetVMConfig` (§6)
does this as a side effect, at no extra cost since it already paid for the
`Config` call.

The index has no bound or sweep of its own in this version — an entry for
a since-deleted VM is only cleared lazily, the next time that exact UUID
is looked up again.

---

## 5. Domain methods

Built as thin filters over `Cluster.List`/`Get` rather than their own
copy of the scan/filter loop:

| Method | Kind | Notes |
|---|:-:|---|
| `Cluster.Check(ctx)` | — | `Version()` plus a resources list, as a liveness/permissions probe |
| `Cluster.List(ctx, kind, opts...)` | any | the cache-aware primitive everything else calls |
| `Cluster.Get(ctx, kind, id)` | VM, LXC, Storage | `List` narrowed to one `id`, `[0]` or `ErrResourceNotFound` |
| `Cluster.GetVMConfig(ctx, vmID)` | VM | `Config`/`Status`; `ErrNodeInaccessible` when the VM's node is unreachable; opportunistically warms the UUID index |
| `Cluster.GetNodeHAGroups(ctx, node)` | — | calls the HA group API directly — HA groups aren't a `cluster.Resources()` shape, so the resource cache doesn't apply |
| `Cluster.GetNextID(ctx, hint)` | — | linear scan on Proxmox's "already in use" response |
| `Cluster.DeleteVM(ctx, vm)` | VM | stops (if running) then deletes |

`GetVMConfig` returns `ErrNodeInaccessible` if the VM's node is reported
`"unknown"` in the cluster resource list — the one case where the caller
needs to distinguish "this cluster is fine but the VM wasn't found" from
"this cluster has a node that couldn't be reached right now."

---

## 6. Errors

```go
var (
	ErrClustersNotFound = errors.New("clusters not found")
	ErrClusterNotFound  = errors.New("cluster not found")
	ErrHAGroupNotFound  = errors.New("ha-group not found")
	ErrInstanceNotFound = errors.New("instance not found")
	ErrNodeInaccessible = errors.New("node is inaccessible")
	ErrResourceNotFound = errors.New("resource not found")
	ErrZoneNotFound     = errors.New("zone not found")
)
```

`ErrResourceNotFound` is `Get`'s sentinel: since `Get` spans every
`ResourceKind`, a not-found storage or an unsupported-kind call returning
an "instance not found" error would be misleading. `ErrInstanceNotFound`
is kept for domain methods that are unambiguously about one VM/LXC guest
(`GetVMConfig`) and don't go through `Get`'s string-typed `id`.
`ErrZoneNotFound` is currently unused by any exported method — carried as
a reserved sentinel, not removed, since dropping an exported error is
itself a breaking change.

---

## 7. Design decisions

**Hand-rolled cache, no external cache dependency.** The cache this
package needs is a map with expiry and a per-key refresh lock, not a
general-purpose, sweep-based cache library. A few dozen lines of generic
Go covers it with no external surface to track for breaking changes.

**TTL-only, no write-through invalidation (this version).** Nothing here
intercepts write calls — they go straight through `pool.Get`/
`Cluster.client()` to the raw REST client — so a create/delete doesn't
invalidate the relevant cache entry immediately. A short TTL is expected
to cover most callers' actual staleness tolerance; write-through
invalidation would mean wrapping the whole write surface for a narrower
benefit.

**A fluent `Cluster` handle instead of a flat `cluster string` parameter
everywhere.** Matches `go-proxmox-rest`'s own house style
(`client.Nodes(node).Qemu().Status(ctx, vmid)`, not
`client.QemuStatus(ctx, node, vmid)`). `pool.Cluster(name)` plays the same
role as `client.Nodes(node)`: a cheap, side-effect-free handle that
threads the selector through a receiver instead of repeating it as an
argument on every call.

**No pool-level cross-cluster search method.** Once `Cluster.List` gained
`WithMatch`/`WithUUID` and UUID resolution became index-backed, a
pool-level "search every cluster" method added nothing beyond a four-line
loop the caller can write itself (§3) — and it means every exported
signature in this module takes only `context.Context`, primitive types,
and `go-proxmox-rest` types, never anything from a caller's own domain
model.

**Caching stops at the `cluster.Resources()` listing.** Only the four
shapes reachable through `Cluster.List` are cached; anything below that
granularity (e.g. a single storage's exact available bytes) is a direct,
uncached call in this version.

---

## 8. Testing

- `cache_test.go` exercises `cacheEntry`/`resourceCache` directly: TTL 0
  never caches, a hit within TTL doesn't re-fetch, concurrent misses
  collapse into one fetch, a refresh error with a warm cache serves stale
  data, and a refresh error with a cold cache propagates.
- `cluster_test.go` exercises `Cluster.List`/`Get`/`GetVMConfig`/
  `GetNodeHAGroups` against `go-proxmox-rest`'s `fakeapi` package (an
  in-memory Proxmox cluster), covering: `ResourceKind` → `ListFilter`
  mapping, client-side `ListOption` filtering, `ErrClusterNotFound` on
  first use against an unconfigured cluster, TTL-cache behavior, and the
  UUID index's warm-hit/wrong-cluster/malformed-id paths.
- `pool_test.go` covers `NewProxmoxPool`'s construction (including
  credentials-from-file and negative-TTL rejection) and `List`/`Get`/
  `Cluster(...).Check` against an unreachable stub.
