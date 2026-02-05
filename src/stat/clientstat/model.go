package clientstat

type ClientIPTop struct {
	IP     string `json:"ip"`
	Count  int64  `json:"count"`
	Device string `json:"device"`
	Region string `json:"region"`
}

type ClientDayStat struct {
	At         int64            `json:"at"` // day bucket, unix seconds (local)
	IPSketch   string           `json:"ipSketch"`
	IPCount    int64            `json:"ipCount"`
	Top        []ClientIPTop    `json:"top"`
	DeviceDist map[string]int64 `json:"deviceDist"`
	RegionDist map[string]int64 `json:"regionDist"`
	UpdatedAt  int64            `json:"updatedAt"`
}

type ClientDistItem struct {
	Name  string `json:"name"`
	Count int64  `json:"count"`
}

type ClientStatCursor struct {
	LastPath string `json:"lastPath"`
	LastLine int64  `json:"lastLine"`
	LastAt   int64  `json:"lastAt"`
}
