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
	"time"

	proxmoxrest "github.com/sergelogvinov/go-proxmox-rest"
)

// poolOptions accumulates every Option passed to NewProxmoxPool.
type poolOptions struct {
	cacheTTL map[ResourceKind]time.Duration
	restOpts []proxmoxrest.Option
}

// Option mutates a pool's construction-time configuration.
type Option func(*poolOptions)

// WithCacheTTL sets the TTL used to cache Cluster.List's underlying
// per-(kind, cluster) listing. TTL 0 (the default) disables caching for
// that kind: every List call goes straight to the API. TTL applies
// uniformly across every cluster in the pool.
func WithCacheTTL(kind ResourceKind, ttl time.Duration) Option {
	return func(o *poolOptions) {
		if o.cacheTTL == nil {
			o.cacheTTL = make(map[ResourceKind]time.Duration)
		}

		o.cacheTTL[kind] = ttl
	}
}

// WithRESTOption forwards one or more proxmoxrest.Option values to every
// per-cluster proxmoxrest.New call this pool makes — the escape hatch for
// WithRoundRobin, WithTimeout, WithLogger, and any other client-level
// option not otherwise exposed here.
func WithRESTOption(opts ...proxmoxrest.Option) Option {
	return func(o *poolOptions) {
		o.restOpts = append(o.restOpts, opts...)
	}
}
