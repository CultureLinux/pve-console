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

type GuestConfig map[string]any

type User struct {
	UserID     string `json:"userid"`
	Enable     Bool   `json:"enable"`
	TFAEnabled bool   `json:"tfa_enabled"`
	Expire     int64  `json:"expire,omitempty"`
	FirstName  string `json:"firstname,omitempty"`
	LastName   string `json:"lastname,omitempty"`
	Email      string `json:"email,omitempty"`
	Comment    string `json:"comment,omitempty"`
}

type TFAEntry struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
	Created     int64  `json:"created,omitempty"`
}

type PackageUpdate struct {
	Name      string `json:"name"`
	Current   string `json:"current,omitempty"`
	Candidate string `json:"candidate,omitempty"`
	Suite     string `json:"suite,omitempty"`
}

type BackupJob struct {
	ID       string `json:"id,omitempty"`
	Storage  string `json:"storage,omitempty"`
	Schedule string `json:"schedule,omitempty"`
	Enabled  Bool   `json:"enabled,omitempty"`
	VMID     string `json:"vmid,omitempty"`
	Mode     string `json:"mode,omitempty"`
	Compress string `json:"compress,omitempty"`
}

type BackupVolume struct {
	VolID  string `json:"volid"`
	VMID   int    `json:"vmid,omitempty"`
	Format string `json:"format,omitempty"`
	Size   uint64 `json:"size,omitempty"`
	CTime  int64  `json:"ctime,omitempty"`
	Notes  string `json:"notes,omitempty"`
}
