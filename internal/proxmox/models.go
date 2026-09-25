package proxmox

type GuestType string

const (
	GuestTypeQEMU GuestType = "qemu"
	GuestTypeLXC  GuestType = "lxc"
)

type Guest struct {
	VMID   int       `json:"vmid"`
	Name   string    `json:"name"`
	Type   GuestType `json:"type"`
	Status string    `json:"status"`
	CPU    float64   `json:"cpu"`
	Memory uint64    `json:"memory"`
	MaxMem uint64    `json:"maxmem"`
	Uptime int64     `json:"uptime"`
}

type NodeInfo struct {
	Name           string `json:"name"`
	ProxmoxVersion string `json:"proxmox_version"`
	Kernel         string `json:"kernel"`
}

type Storage struct {
	Name    string  `json:"storage"`
	Type    string  `json:"type"`
	Used    uint64  `json:"used"`
	Total   uint64  `json:"total"`
	Enabled Bool    `json:"enabled"`
	Active  Bool    `json:"active"`
	Usage   float64 `json:"usage"`
}

type Snapshot struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	SnapTime    int64  `json:"snaptime,omitempty"`
	VMState     Bool   `json:"vmstate,omitempty"`
}
