package host

type ResUsage struct {
	AppCpu    string `json:"appCpu"`    // Application CPU usage in percentage
	AppMem    string `json:"appMem"`    // Application Memory usage in percentage
	AppDisk   string `json:"appDisk"`   // Application Disk usage in percentage
	CPU       string `json:"cpu"`       // CPU usage in percentage
	Mem       string `json:"mem"`       // Memory usage in percentage
	TotalMem  string `json:"totalMem"`  // Total Memory in MB
	Disk      string `json:"disk"`      // Disk usage in percentage
	TotalDisk string `json:"totalDisk"` // Total Disk in MB
	At        int64  `json:"at"`        // Timestamp of the usage data
}

type ResType string

const (
	ResTypeCPU       ResType = "cpu"       // CPU usage
	ResTypeAppCPU    ResType = "appCpu"    // Application CPU usage
	ResTypeMemory    ResType = "mem"       // Memory usage
	ResTypeAppMem    ResType = "appMem"    // Application Memory usage
	ResTypeTotalMem  ResType = "totalMem"  // Total Memory usage
	ResTypeDisk      ResType = "disk"      // Disk usage
	ResTypeAppDisk   ResType = "appDisk"   // Application Disk usage
	ResTypeTotalDisk ResType = "totalDisk" // Total Disk usage
)

func (r ResType) String() string {
	return string(r)
}
