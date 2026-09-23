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
	"fmt"
	"os"
	"strings"
	"sync"

	proxmoxrest "github.com/sergelogvinov/go-proxmox-rest"
)

// ClusterConfig defines a single Proxmox cluster's connection
// configuration.
type ClusterConfig struct {
	// Name is an optional display label — metadata only.
	Name string `yaml:"name,omitempty"`
	// Region is the selector: pool.List()/pool.Get()/pool.Cluster()
	// all key on this value.
	Region string `yaml:"region,omitempty"`
	// Proxmox REST API connection details.
	URL      string `yaml:"url"`
	CAFile   string `yaml:"ca_file,omitempty"`
	Insecure bool   `yaml:"insecure,omitempty"`
	TokenID  string `yaml:"token_id,omitempty"`
	TokenIDFile     string `yaml:"token_id_file,omitempty"`
	TokenSecret     string `yaml:"token_secret,omitempty"`
	TokenSecretFile string `yaml:"token_secret_file,omitempty"`
	Username        string `yaml:"username,omitempty"`
	Password        string `yaml:"password,omitempty"`
}

// ProxmoxPool is a pool of Proxmox REST clients, one per configured
// cluster, plus the shared resource cache and UUID index every Cluster
// handle obtained from it reads and writes.
type ProxmoxPool struct {
	clients   map[string]*proxmoxrest.Client
	cache     *resourceCache
	uuidIndex sync.Map // string (uuid) -> uuidIndexEntry
}

// NewProxmoxPool creates a new Proxmox cluster client pool.
func NewProxmoxPool(config []*ClusterConfig, options ...Option) (*ProxmoxPool, error) {
	if len(config) == 0 {
		return nil, ErrClustersNotFound
	}

	var o poolOptions

	for _, opt := range options {
		if opt != nil {
			opt(&o)
		}
	}

	for kind, ttl := range o.cacheTTL {
		if ttl < 0 {
			return nil, fmt.Errorf("proxmoxpool: negative cache TTL for resource kind %d", kind)
		}
	}

	clients := make(map[string]*proxmoxrest.Client, len(config))

	for _, cfg := range config {
		if cfg.TokenID == "" && cfg.TokenIDFile != "" {
			var err error

			cfg.TokenID, err = readValueFromFile(cfg.TokenIDFile)
			if err != nil {
				return nil, err
			}
		}

		if cfg.TokenSecret == "" && cfg.TokenSecretFile != "" {
			var err error

			cfg.TokenSecret, err = readValueFromFile(cfg.TokenSecretFile)
			if err != nil {
				return nil, err
			}
		}

		restOpts := []proxmoxrest.Option{
			proxmoxrest.WithURL(cfg.URL),
			proxmoxrest.WithInsecure(cfg.Insecure),
			proxmoxrest.WithUserAgent("go-proxmox-pool/1.0"),
		}

		if cfg.CAFile != "" {
			restOpts = append(restOpts, proxmoxrest.WithCACert(cfg.CAFile))
		}

		if cfg.Username != "" && cfg.Password != "" {
			restOpts = append(restOpts, proxmoxrest.WithPasswordAuth(cfg.Username, cfg.Password))
		} else if cfg.TokenID != "" && cfg.TokenSecret != "" {
			restOpts = append(restOpts, proxmoxrest.WithTokenAuth(cfg.TokenID, cfg.TokenSecret))
		}

		restOpts = append(restOpts, o.restOpts...)

		client, err := proxmoxrest.New(proxmoxrest.ClientConfig{}, restOpts...)
		if err != nil {
			return nil, fmt.Errorf("failed to create Proxmox REST client for cluster %s: %w", cfg.Region, err)
		}

		clients[cfg.Region] = client
	}

	return &ProxmoxPool{
		clients: clients,
		cache:   newResourceCache(o.cacheTTL),
	}, nil
}

// List returns the names of every cluster configured in the pool.
func (p *ProxmoxPool) List() []string {
	names := make([]string, 0, len(p.clients))

	for name := range p.clients {
		names = append(names, name)
	}

	return names
}

// Get returns the raw go-proxmox-rest client for the named cluster — the
// escape hatch for call sites that need the full client surface.
func (p *ProxmoxPool) Get(cluster string) (*proxmoxrest.Client, error) {
	if client, ok := p.clients[cluster]; ok {
		return client, nil
	}

	return nil, ErrClusterNotFound
}

// Set overrides (or injects) the REST client for a cluster. Intended for
// tests that point a cluster at an in-memory fake server after the pool was
// already built from static config.
func (p *ProxmoxPool) Set(cluster string, client *proxmoxrest.Client) {
	p.clients[cluster] = client
}

// Cluster returns a handle scoped to the named cluster. It never itself
// fails — no client-side existence check is performed; ErrClusterNotFound
// surfaces from the handle's first actual call instead.
func (p *ProxmoxPool) Cluster(cluster string) *Cluster {
	return &Cluster{pool: p, name: cluster}
}

func readValueFromFile(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("path cannot be empty")
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read file '%s': %w", path, err)
	}

	return strings.TrimSpace(string(content)), nil
}
