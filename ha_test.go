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
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"

	proxmoxrest "github.com/sergelogvinov/go-proxmox-rest"
)

func TestIsHAGroupsMigrated(t *testing.T) {
	assert.True(t, isHAGroupsMigrated(&proxmoxrest.APIError{
		StatusCode: http.StatusInternalServerError,
		Message:    "500 cannot index groups: ha groups have been migrated to rules",
	}))

	// Message casing shouldn't matter.
	assert.True(t, isHAGroupsMigrated(&proxmoxrest.APIError{
		StatusCode: http.StatusInternalServerError,
		Message:    "HA GROUPS HAVE BEEN MIGRATED TO RULES",
	}))

	// Wrapped errors must still be recognized.
	wrapped := errors.Join(errors.New("context"), &proxmoxrest.APIError{
		StatusCode: http.StatusInternalServerError,
		Message:    "ha groups have been migrated to rules",
	})
	assert.True(t, isHAGroupsMigrated(wrapped))

	// Any other 500 is a real error, not a migration signal.
	assert.False(t, isHAGroupsMigrated(&proxmoxrest.APIError{
		StatusCode: http.StatusInternalServerError,
		Message:    "some other failure",
	}))

	// A matching message on the wrong status code doesn't count.
	assert.False(t, isHAGroupsMigrated(&proxmoxrest.APIError{
		StatusCode: http.StatusNotFound,
		Message:    "ha groups have been migrated to rules",
	}))

	assert.False(t, isHAGroupsMigrated(errors.New("plain error")))
	assert.False(t, isHAGroupsMigrated(nil))
}

func TestNodeInNodesList(t *testing.T) {
	assert.True(t, nodeInNodesList("pve1", "pve1"))
	assert.True(t, nodeInNodesList("pve1", "pve1:2,pve2:1"))
	assert.True(t, nodeInNodesList("pve2", "pve1:2,pve2:1"))
	assert.False(t, nodeInNodesList("pve3", "pve1:2,pve2:1"))
	assert.False(t, nodeInNodesList("pve1", ""))
}
