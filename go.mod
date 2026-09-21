module github.com/sergelogvinov/go-proxmox-pool

go 1.27.1

// replace github.com/sergelogvinov/go-proxmox-rest => ../proxmox/go-proxmox-rest

require (
	github.com/sergelogvinov/go-proxmox-rest v0.0.0-20260922140521-cb1976ec06d4
	github.com/stretchr/testify v1.12.1
)

require (
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/net v0.59.0 // indirect
	resty.dev/v3 v3.0.0-rc.4 // indirect
)
