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
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	proxmoxrest "github.com/sergelogvinov/go-proxmox-rest"
	"github.com/sergelogvinov/go-proxmox-rest/cluster/ha"
)

// haGroupsMigratedMessage is the substring Proxmox's
// GET /cluster/ha/groups returns (as a 500) once HA groups have been
// migrated to HA rules, e.g. "cannot index groups: ha groups have been
// migrated to rules".
const haGroupsMigratedMessage = "ha groups have been migrated to rules"

// isHAGroupsMigrated reports whether err is the 500 Proxmox returns from
// GET /cluster/ha/groups once the cluster's HA groups have been migrated to
// HA rules.
func isHAGroupsMigrated(err error) bool {
	var apiErr *proxmoxrest.APIError

	return errors.As(err, &apiErr) &&
		apiErr.StatusCode == http.StatusInternalServerError &&
		strings.Contains(strings.ToLower(apiErr.Message), haGroupsMigratedMessage)
}

// GetNodeHAGroups returns the sorted list of HA groups the given node
// belongs to, or ErrHAGroupNotFound if it belongs to none. HA groups aren't
// a cluster.Resources() shape, so this calls the HA API directly rather
// than going through Cluster.List/the resource cache.
//
// Proxmox 9 replaced HA groups with HA rules: clusters that have been
// migrated reject GET /cluster/ha/groups with a 500 (see
// isHAGroupsMigrated), so on that specific error this falls back to
// listing "node-affinity" HA rules instead, which are the migrated
// equivalent of groups (Rule.Rule/Rule.Nodes standing in for
// Group.Group/Group.Nodes).
//
// +proxmox:rbac:feature=hagroup
func (c *Cluster) GetNodeHAGroups(ctx context.Context, node string) ([]string, error) {
	px, err := c.client()
	if err != nil {
		return nil, err
	}

	groups := []string{}

	haGroups, err := px.Cluster().HA().Groups().List(ctx)
	switch {
	case err == nil:
		for _, g := range haGroups {
			if g.Type != "group" {
				continue
			}

			if nodeInNodesList(node, g.Nodes) {
				groups = append(groups, g.Group)
			}
		}

	case isHAGroupsMigrated(err):
		haRules, rulesErr := px.Cluster().HA().Rules().List(ctx, ha.RuleTypeNodeAffinity, "")
		if rulesErr != nil {
			return nil, fmt.Errorf("error get ha-rules %w", rulesErr)
		}

		for _, r := range haRules {
			if r.Type != string(ha.RuleTypeNodeAffinity) {
				continue
			}

			if nodeInNodesList(node, r.Nodes) {
				groups = append(groups, r.Rule)
			}
		}

	default:
		return nil, fmt.Errorf("error get ha-groups %w", err)
	}

	if len(groups) == 0 {
		return nil, ErrHAGroupNotFound
	}

	slices.Sort(groups)

	return groups, nil
}

// nodeInNodesList reports whether node appears in nodes, a comma-separated
// property-string list of node names with optional ":priority" suffixes
// (e.g. "node1:2,node2:1"), as used by both Group.Nodes and Rule.Nodes.
func nodeInNodesList(node, nodes string) bool {
	for n := range strings.SplitSeq(nodes, ",") {
		if node == strings.Split(n, ":")[0] {
			return true
		}
	}

	return false
}
