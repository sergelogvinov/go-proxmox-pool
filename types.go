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

// VMDetails holds the VM config/status fields consumers need to build
// instance metadata: its node, live CPU/memory, and SMBIOS-derived UUID and
// instance type.
type VMDetails struct {
	VMID   int
	Node   string
	Name   string
	CPUs   int
	MaxMem int64
	UUID   string
	Type   string
}
