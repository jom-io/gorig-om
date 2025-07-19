package logtool

var Levels = []Level{DebugLevel, InfoLevel, WarnLevel, ErrorLevel, FatalLevel, DpanicLevel}

type Level string

type SearchOptions struct {
	Categories []string `json:"categories" form:"categories"`
	Level      string   `json:"level" form:"level"`
	Levels     []string `json:"levels" form:"levels"`
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

const (
	DebugLevel  Level = "debug"
	InfoLevel   Level = "info"
	WarnLevel   Level = "warn"
	ErrorLevel  Level = "error"
	FatalLevel  Level = "fatal"
	DpanicLevel Level = "dpanic"
)

func (l Level) Str() string {
	return string(l)
}
