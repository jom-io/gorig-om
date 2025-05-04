package logtool

var Categories = []string{"commons", "rest", "console"}

var Levels = []string{"debug", "info", "warn", "error", "fatal", "dpanic"}

type SearchOptions struct {
	Categories []string `json:"categories" form:"categories"`
	Level      string   `json:"level" form:"level"`
	TraceID    string   `json:"traceID" form:"traceID"`
	Keyword    string   `json:"keyword" form:"keyword"`
	StartTime  string   `json:"startTime" form:"startTime"`
	EndTime    string   `json:"endTime" form:"endTime"`

	RootDir  string `json:"root_dir" form:"rootDir"`
	Size     int    `json:"size" form:"size"`
	LastPath string `json:"lastPath" form:"lastPath"`
	LastLine int64  `json:"lastLine" form:"lastLine"`
}

type MatchedRecord struct {
	FilePath   string     `json:"path"`
	LineNumber int64      `json:"line"`
	Record     *LogRecord `json:"record"`
}

func (m MatchedRecord) ToJsonStr() string {
	return m.Record.ToJsonStr()
}
