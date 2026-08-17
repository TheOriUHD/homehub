package pve

// VersionInfo is the API server's version, from GET /version.
type VersionInfo struct {
	Version string `json:"version"`
	Release string `json:"release"`
	RepoID  string `json:"repoid"`
}

// ClusterStatusEntry is one row of GET /cluster/status: the cluster itself
// (type "cluster", carries Quorate) or a node (type "node", carries Online/IP).
type ClusterStatusEntry struct {
	Type    string   `json:"type"`
	Name    string   `json:"name"`
	Online  FlexBool `json:"online"`
	Quorate FlexBool `json:"quorate"`
	IP      string   `json:"ip"`
}

// Resource is one row of GET /cluster/resources — a node, guest, or storage
// depending on Type ("node", "qemu", "lxc", "storage").
type Resource struct {
	ID       string    `json:"id"`
	Type     string    `json:"type"`
	Node     string    `json:"node"`
	VMID     FlexInt64 `json:"vmid"`
	Name     string    `json:"name"`
	Status   string    `json:"status"`
	CPU      FlexFloat `json:"cpu"`
	MaxCPU   FlexFloat `json:"maxcpu"`
	Mem      FlexInt64 `json:"mem"`
	MaxMem   FlexInt64 `json:"maxmem"`
	Disk     FlexInt64 `json:"disk"`
	MaxDisk  FlexInt64 `json:"maxdisk"`
	Uptime   FlexInt64 `json:"uptime"`
	Template FlexBool  `json:"template"`
	Tags     string    `json:"tags"`
	Storage  string    `json:"storage"`
}

// MemoryInfo is a node's memory usage in bytes.
type MemoryInfo struct {
	Used  FlexInt64 `json:"used"`
	Total FlexInt64 `json:"total"`
	Free  FlexInt64 `json:"free"`
}

// NodeStatus is GET /nodes/{node}/status.
type NodeStatus struct {
	CPU        FlexFloat  `json:"cpu"`
	Memory     MemoryInfo `json:"memory"`
	LoadAvg    []string   `json:"loadavg"`
	Uptime     FlexInt64  `json:"uptime"`
	PVEVersion string     `json:"pveversion"`
	KVersion   string     `json:"kversion"`
}

// GuestStatus is GET /nodes/{node}/{kind}/{vmid}/status/current.
type GuestStatus struct {
	Status    string    `json:"status"`
	Name      string    `json:"name"`
	QMPStatus string    `json:"qmpstatus"`
	CPU       FlexFloat `json:"cpu"`
	CPUs      FlexFloat `json:"cpus"`
	Mem       FlexInt64 `json:"mem"`
	MaxMem    FlexInt64 `json:"maxmem"`
	Disk      FlexInt64 `json:"disk"`
	MaxDisk   FlexInt64 `json:"maxdisk"`
	Uptime    FlexInt64 `json:"uptime"`
	PID       FlexInt64 `json:"pid"`
	Agent     FlexBool  `json:"agent"`
	Lock      string    `json:"lock"`
	Tags      string    `json:"tags"`
}

// Snapshot is one guest snapshot. SnapTime is a Unix timestamp; VMState
// reports whether RAM was included.
type Snapshot struct {
	Name        string    `json:"name"`
	Description string    `json:"description"`
	SnapTime    FlexInt64 `json:"snaptime"`
	VMState     FlexBool  `json:"vmstate"`
	Parent      string    `json:"parent"`
}

// Task is one row of GET /nodes/{node}/tasks.
type Task struct {
	UPID       string    `json:"upid"`
	Type       string    `json:"type"`
	ID         string    `json:"id"`
	User       string    `json:"user"`
	Status     string    `json:"status"`
	ExitStatus string    `json:"exitstatus"`
	StartTime  FlexInt64 `json:"starttime"`
	EndTime    FlexInt64 `json:"endtime"`
}

// TaskStatus is GET /nodes/{node}/tasks/{upid}/status. Status is "running"
// until the task ends, then "stopped"; ExitStatus is "OK" on success or an
// error message otherwise.
type TaskStatus struct {
	UPID       string    `json:"upid"`
	Type       string    `json:"type"`
	ID         string    `json:"id"`
	User       string    `json:"user"`
	Node       string    `json:"node"`
	PID        FlexInt64 `json:"pid"`
	Status     string    `json:"status"`
	ExitStatus string    `json:"exitstatus"`
	StartTime  FlexInt64 `json:"starttime"`
}

// Finished reports whether the task has stopped running.
func (t TaskStatus) Finished() bool { return t.Status == "stopped" }

// OK reports whether the task finished successfully.
func (t TaskStatus) OK() bool { return t.Finished() && t.ExitStatus == "OK" }

// TaskLogLine is one line of task output; N is the 1-based line number.
type TaskLogLine struct {
	N FlexInt64 `json:"n"`
	T string    `json:"t"`
}

// Storage is one row of GET /nodes/{node}/storage. Sizes are bytes.
type Storage struct {
	Storage      string    `json:"storage"`
	Type         string    `json:"type"`
	Content      string    `json:"content"`
	Active       FlexBool  `json:"active"`
	Enabled      FlexBool  `json:"enabled"`
	Shared       FlexBool  `json:"shared"`
	Used         FlexInt64 `json:"used"`
	Avail        FlexInt64 `json:"avail"`
	Total        FlexInt64 `json:"total"`
	UsedFraction FlexFloat `json:"used_fraction"`
}

// BackupJob is one row of GET /cluster/backup.
type BackupJob struct {
	ID       string    `json:"id"`
	Type     string    `json:"type"`
	Enabled  FlexBool  `json:"enabled"`
	Schedule string    `json:"schedule"`
	Storage  string    `json:"storage"`
	Node     string    `json:"node"`
	VMID     string    `json:"vmid"`
	All      FlexBool  `json:"all"`
	Mode     string    `json:"mode"`
	Compress string    `json:"compress"`
	NextRun  FlexInt64 `json:"next-run"`
	Comment  string    `json:"comment"`
	MailTo   string    `json:"mailto"`
	Prune    FlexPropString `json:"prune-backups"`
}

// BackupJobSpec is the writable subset of a vzdump backup job for
// CreateBackupJob and UpdateBackupJob. Empty string fields are omitted.
type BackupJobSpec struct {
	// Schedule is a calendar event spec, e.g. "sun 02:00".
	Schedule string
	Storage  string
	// Node restricts the job to one node; empty means all nodes.
	Node string
	// VMID is a comma-separated guest id list; ignored when All is set.
	VMID string
	All  bool
	// Mode is "snapshot", "suspend", or "stop".
	Mode string
	// Compress is "0", "gzip", "lzo", or "zstd".
	Compress string
	Enabled  bool
	Comment  string
	MailTo   string
	// Prune is a prune-backups spec, e.g. "keep-last=3,keep-weekly=2".
	Prune string
}

// VNCProxyTicket is the result of POST .../vncproxy.
type VNCProxyTicket struct {
	Ticket string    `json:"ticket"`
	Port   FlexInt64 `json:"port"`
	User   string    `json:"user"`
	Cert   string    `json:"cert"`
}

// TermProxyTicket is the result of POST .../termproxy.
type TermProxyTicket struct {
	Ticket string    `json:"ticket"`
	Port   FlexInt64 `json:"port"`
	User   string    `json:"user"`
}
