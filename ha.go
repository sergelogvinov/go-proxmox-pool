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
	"slices"
	"strings"
)

// GetNodeHAGroups returns the sorted list of HA groups the given node
// belongs to, or ErrHAGroupNotFound if it belongs to none. HA groups aren't
// a cluster.Resources() shape, so this calls the HA API directly rather
// than going through Cluster.List/the resource cache.
//
// +proxmox:rbac:feature=hagroup
func (c *Cluster) GetNodeHAGroups(ctx context.Context, node string) ([]string, error) {
	px, err := c.client()
	if err != nil {
		return nil, err
	}

	haGroups, err := px.Cluster().HA().Groups().List(ctx)
	if err != nil {
		return nil, fmt.Errorf("error get ha-groups %w", err)
	}

	groups := []string{}

	for _, g := range haGroups {
		if g.Type != "group" {
			continue
		}

		for n := range strings.SplitSeq(g.Nodes, ",") {
			if node == strings.Split(n, ":")[0] {
				groups = append(groups, g.Group)
			}
		}
	}

	if len(groups) == 0 {
		return nil, ErrHAGroupNotFound
	}

	slices.Sort(groups)

	return groups, nil
}
