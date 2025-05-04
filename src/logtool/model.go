package logtool

import (
	"encoding/json"
	"errors"
	"time"
)

type LogRecord struct {
	Level   string            `json:"level"`
	Time    string            `json:"time"`
	Msg     string            `json:"msg"`
	TraceID string            `json:"_trace_id_"`
	Error   string            `json:"error"`
	Data    map[string]string `json:"data"`
}

func (r *LogRecord) ParsedTime() (time.Time, error) {
	layout := "2006-01-02 15:04:05.000"
	return time.Parse(layout, r.Time)
}

func (r *LogRecord) Validate() error {
	if r.Time == "" {
		return errors.New("invalid log: missing time")
	}
	return nil
}

func (r *LogRecord) ToJsonStr() string {
	str, _ := json.Marshal(r)
	return string(str)
}
