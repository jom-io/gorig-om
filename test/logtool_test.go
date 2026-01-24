package test

import (
	"context"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/jom-io/gorig-om/src/logtool"
	"github.com/rs/xid"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestListLogFiles(t *testing.T) {
	opts := logtool.SearchOptions{}
	opts.RootDir = "../../"

	opts.Categories = []string{"rest", "console", "commons"}
	result, err := logtool.ListLogFiles(opts)
	if err != nil {
		t.Errorf("ListLogFiles() error = %v", err)
		return
	}
	fmt.Printf("all matched files: %d\n", len(result))
	for i, file := range result {
		fmt.Printf("#%s => %s\n", i, file)
	}

	filePath := fmt.Sprintf("../../.logs/commons/commons-%s.jsonl", time.Now().Format("2006-01-02T15-04-05.000"))
	f, err := os.Create(filePath)
	if err != nil {
		t.Errorf("create file error: %v", err)
		return
	}
	defer f.Close()

	opts.Categories = []string{}
	result, err = logtool.ListLogFiles(opts)
	if err != nil {
		t.Errorf("ListLogFiles() error = %v", err)
		return
	}
	fmt.Printf("all matched files: %d\n", len(result))
	for i, file := range result {
		fmt.Printf("#%s => %s\n", i, file)
	}

	opts.EndTime = time.Now().Format("2006-01-02 15:04:05")
	result, err = logtool.ListLogFiles(opts)
	if err != nil {
		t.Errorf("ListLogFiles() error = %v", err)
		return
	}
	fmt.Printf("end time matched files: %d\n", len(result))
	for i, file := range result {
		fmt.Printf("#%s => %s\n", i, file)
	}
	err = os.Remove(filePath)
}

func TestSearchLog(t *testing.T) {
	opts := logtool.SearchOptions{}
	opts.RootDir = "../../"
	//opts.LastPath = "../../.logs/rest/rest.jsonl"
	//opts.LastLine = 130
	opts.Size = 20
	//opts.StartTime = "2024-03-19 22:59:10"
	//opts.EndTime = "2024-04-02 22:59:10"
	//opts.Categories = []string{"rest"}
	//opts.TraceID = "culek396egfrij35e4q0"
	//opts.Level = "error"
	opts.Keyword = "BindParams"
	result, err := logtool.SearchLogs(opts)
	if err != nil {
		t.Errorf("SearchLogs() error = %v", err)
		return
	}
	fmt.Printf("all matched records: %d\n", len(result))
	for i, rec := range result {
		fmt.Printf("#%d => file: %s  line: %d  record:%s\n",
			i+1, rec.FilePath, rec.LineNumber, rec.Record.ToJsonStr())
	}
}

func TestSearchLogNear(t *testing.T) {
	centerLine := int64(20)
	rangeLine := int64(10)
	result, err := logtool.FetchContextLines("../../.logs/commons/commons.jsonl", centerLine, rangeLine)
	if err != nil {
		t.Errorf("FetchContextLines() error = %v", err)
		return
	}
	for i, rec := range result {
		if rec.LineNumber == centerLine {
			fmt.Printf("# current => file: %s  line: %d  record:%s\n",
				rec.FilePath, rec.LineNumber, rec.Record.ToJsonStr())
		} else {
			fmt.Printf("#%d => file: %s  line: %d  record:%s\n",
				i+1, rec.FilePath, rec.LineNumber, rec.Record.ToJsonStr())
		}
	}
}

func TestMonitorLogs(t *testing.T) {
	opts := logtool.SearchOptions{}
	opts.RootDir = "../../"
	go func() {
		filePath := "../../.logs/commons/commons.jsonl"
		f, err := os.OpenFile(filePath, os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			t.Errorf("open file error: %v", err)
			return
		}
		defer f.Close()
		for i := 0; i < 10; i++ {
			time.Sleep(time.Millisecond * 100)
			timeStr := time.Now().Format("2006-01-02 15:04:05.000")
			_, err = f.WriteString(fmt.Sprintf("{\"time\":\"%s\",\"level\":\"info\",\"msg\":\"test log %d\"}\n", timeStr, i))
			if err != nil {
				t.Errorf("write file error: %v", err)
				return
			}
		}
	}()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req := httptest.NewRequest("GET", "/", nil)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	go func() {
		err := logtool.MonitorLogs(c, opts)
		if err != nil {
			t.Errorf("MonitorLogs() error = %v", err)
		}
	}()
	go func() {
		lastBodyLen := 0
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(10 * time.Millisecond):
				body := w.Body.String()
				if len(body) > lastBodyLen {
					fmt.Printf(body[lastBodyLen:])
					lastBodyLen = len(body)
				}
			}
		}
	}()
	time.Sleep(10 * time.Second)
	cancel()

}

// TestFromTranceID tests getting trace ID from context.
func TestFromTranceID(t *testing.T) {
	id := xid.New().String()
	form, e := xid.FromString(id)
	if e != nil {
		t.Errorf("FromString() error = %v", e)
	}
	t.Logf("original id: %s, from string: %s", id, form.Time())
}
