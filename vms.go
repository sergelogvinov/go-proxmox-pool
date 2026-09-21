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
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"time"

	proxmoxrest "github.com/sergelogvinov/go-proxmox-rest"
	pxcluster "github.com/sergelogvinov/go-proxmox-rest/cluster"
	"github.com/sergelogvinov/go-proxmox-rest/nodes/qemu"
	"github.com/sergelogvinov/go-proxmox-rest/nodes/tasks"
)

// powerActionTimeout bounds how long a stop/delete task is waited on.
const powerActionTimeout = time.Minute

// GetVMConfig returns a VM's config and live status by its ID. Returns
// ErrNodeInaccessible if the VM's node is unreachable (reported as Status
// "unknown" in the cluster resource list). As a side effect, it warms the
// pool's UUID index whenever the VM's config carries an SMBIOS UUID, so a
// later WithUUID lookup for this VM is a free index hit.
//
// +proxmox:rbac:feature=base
func (c *Cluster) GetVMConfig(ctx context.Context, vmID int) (*VMDetails, error) {
	resources, err := c.List(ctx, ResourceKindVM, WithVMID(vmID), SkipTemplates())
	if err != nil {
		return nil, err
	}

	if len(resources) == 0 {
		return nil, ErrInstanceNotFound
	}

	rs := &resources[0]

	if rs.Status == "unknown" {
		return nil, ErrNodeInaccessible
	}

	px, err := c.client()
	if err != nil {
		return nil, err
	}

	cfg, err := px.Nodes(rs.Node).Qemu().Config(ctx, vmID, nil)
	if err != nil {
		return nil, err
	}

	status, err := px.Nodes(rs.Node).Qemu().Status(ctx, vmID)
	if err != nil {
		return nil, err
	}

	details := &VMDetails{
		VMID:   rs.VMID,
		Node:   rs.Node,
		Name:   status.Name,
		CPUs:   status.CPUs,
		MaxMem: status.MaxMem,
	}

	if cfg.SMBios1 != nil {
		details.UUID = cfg.SMBios1.UUID

		if details.UUID != "" {
			c.pool.uuidStore(details.UUID, uuidIndexEntry{cluster: c.name, vmID: rs.VMID})
		}

		if sku, err := base64.StdEncoding.DecodeString(cfg.SMBios1.SKU); err == nil {
			details.Type = string(sku)
		}
	}

	return details, nil
}

// GetNextID finds the next unused VMID starting at hint, incrementing and
// retrying on Proxmox's "already in use" (400) response.
func (c *Cluster) GetNextID(ctx context.Context, hint int) (int, error) {
	px, err := c.client()
	if err != nil {
		return 0, err
	}

	for {
		id, err := px.Cluster().NextID(ctx, hint)
		if err == nil {
			return id, nil
		}

		var apiErr *proxmoxrest.APIError
		if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusBadRequest {
			return 0, err
		}

		hint++
	}
}

// DeleteVM stops (if running) and deletes a VM.
func (c *Cluster) DeleteVM(ctx context.Context, vm *pxcluster.Resource) error {
	px, err := c.client()
	if err != nil {
		return err
	}

	status, err := px.Nodes(vm.Node).Qemu().Status(ctx, vm.VMID)
	if err != nil {
		return err
	}

	if status.Status == qemu.VMStatusRunning {
		upid, err := px.Nodes(vm.Node).Qemu().Stop(ctx, vm.VMID, nil)
		if err != nil {
			return fmt.Errorf("failed to stop vm %d: %w", vm.VMID, err)
		}

		if upid != "" {
			if err := px.Nodes(vm.Node).Tasks().Wait(ctx, upid, &tasks.WaitOptions{Timeout: powerActionTimeout}); err != nil {
				return fmt.Errorf("unable to stop vm %d: %w", vm.VMID, err)
			}
		}
	}

	upid, err := px.Nodes(vm.Node).Qemu().Delete(ctx, vm.VMID, nil)
	if err != nil {
		return fmt.Errorf("cannot delete vm with id %d: %w", vm.VMID, err)
	}

	if upid == "" {
		return nil
	}

	return px.Nodes(vm.Node).Tasks().Wait(ctx, upid, &tasks.WaitOptions{Timeout: powerActionTimeout})
}
