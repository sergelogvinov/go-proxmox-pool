# Proxmox Pool

> **Status:** This project is under active development. Its API may change and
> breaking changes are possible. Do not use it in production yet.

`go-proxmox-pool` is a Go library that manages a pool of Proxmox REST clients for multiple clusters.
It builds on top of [`go-proxmox-rest`](https://github.com/sergelogvinov/go-proxmox-rest), 
a type-safe Go client for the [Proxmox VE REST API](https://pve.proxmox.com/pve-docs/api-viewer/).

## Projects using this module

- [Proxmox CCM](https://github.com/sergelogvinov/proxmox-cloud-controller-manager)
- [Proxmox CSI](https://github.com/sergelogvinov/proxmox-csi-plugin)
- [Proxmox MCP server](https://github.com/sergelogvinov/proxmox-mcp)
- [Karpenter for Proxmox](https://github.com/sergelogvinov/karpenter-provider-proxmox)

## License

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

[http://www.apache.org/licenses/LICENSE-2.0](http://www.apache.org/licenses/LICENSE-2.0)

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

---

`Proxmox®` is a registered trademark of [Proxmox Server Solutions GmbH](https://www.proxmox.com/en/about/company).
