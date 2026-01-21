package test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
	"unsafe"

	"github.com/jom-io/gorig-om/src/logtool"
	"github.com/jom-io/gorig-om/src/stat/apistat"
	"github.com/jom-io/gorig/cache"
)

func TestApiStatRestLogFile(t *testing.T) {
	ctx := context.Background()
	logPath := filepath.Join(".", ".logs", "rest", "rest.jsonl")
	if _, err := os.Stat(logPath); err != nil {
		t.Skipf("rest log file not found: %s", logPath)
	}

	lastTime, err := readLastLogTime(logPath)
	if err != nil {
		t.Skipf("skip: unable to read last log time: %v", err)
	}
	if lastTime.IsZero() {
		t.Skip("skip: last log time is zero")
	}

	endTime := lastTime.Add(1 * time.Second)
	startTime := lastTime.Add(-10 * time.Minute)

	opts := logtool.SearchOptions{
		RootDir:    ".",
		Categories: []string{"rest"},
		StartTime:  startTime.Format("2006-01-02 15:04:05"),
		EndTime:    endTime.Format("2006-01-02 15:04:05"),
		Levels:     []string{logtool.InfoLevel.Str(), logtool.WarnLevel.Str(), logtool.ErrorLevel.Str()},
		Size:       50000,
	}
	records, errSearch := logtool.SearchLogs(opts)
	if errSearch != nil {
		t.Fatalf("SearchLogs failed: %v", errSearch)
	}
	if len(records) == 0 {
		t.Skip("skip: no rest logs found in time window")
	}

	agg, totals := buildRestAggregates(records)
	if len(agg) == 0 {
		t.Skip("skip: no IN/OUT pairs found in time window")
	}

	storage := getApiLatencyStorage(t, apistat.S())
	nowBucket := time.Now().Add(3650 * 24 * time.Hour).Truncate(time.Minute).Unix()
	for _, stat := range agg {
		stat.At = nowBucket
		cond := map[string]any{
			"at":     stat.At,
			"method": stat.Method,
			"uri":    stat.URI,
		}
		_ = storage.Delete(cond)
		if err := storage.Put(stat); err != nil {
			t.Fatalf("storage put failed: %v", err)
		}
	}

	start := nowBucket - 60
	end := nowBucket + 60

	t.Run("TopPage", func(t *testing.T) {
		page, err := apistat.S().TopPage(ctx, start, end, 1, int64(len(agg)), nil, nil, "", "", nil, "count", false)
		if err != nil {
			t.Fatalf("TopPage failed: %v", err)
		}
		if page == nil || len(page.Items) == 0 {
			t.Fatalf("TopPage returned empty result")
		}
		if int64(len(page.Items)) != int64(len(agg)) {
			t.Fatalf("TopPage item count mismatch: got=%d want=%d", len(page.Items), len(agg))
		}

		for _, item := range page.Items {
			exp, ok := agg[item.Method+"|"+item.URI]
			if !ok {
				t.Fatalf("unexpected item: %s %s", item.Method, item.URI)
			}
			if item.Count != exp.Count || item.Count2xx != exp.Count2xx || item.Count4xx != exp.Count4xx || item.Count5xx != exp.Count5xx {
				t.Fatalf("counts mismatch for %s %s: got=%d/%d/%d/%d",
					item.Method, item.URI, item.Count, item.Count2xx, item.Count4xx, item.Count5xx)
			}
			wantSuccess := float64(exp.Count2xx) / float64(exp.Count)
			if math.Abs(item.SuccessRate-wantSuccess) > 0.0001 {
				t.Fatalf("successRate mismatch for %s %s: got=%v want=%v", item.Method, item.URI, item.SuccessRate, wantSuccess)
			}
		}
	})

	t.Run("TimeRange", func(t *testing.T) {
		items, err := apistat.S().TimeRange(ctx, start, end, cache.GranularityMinute, apistat.ApiStatCount2xx, apistat.ApiStatCount4xx, apistat.ApiStatCount5xx)
		if err != nil {
			t.Fatalf("TimeRange failed: %v", err)
		}
		if len(items) == 0 {
			t.Fatalf("TimeRange returned empty result")
		}
		var total2xx, total4xx, total5xx int64
		for _, item := range items {
			total2xx += int64(item.Value[apistat.ApiStatCount2xx.String()])
			total4xx += int64(item.Value[apistat.ApiStatCount4xx.String()])
			total5xx += int64(item.Value[apistat.ApiStatCount5xx.String()])
		}
		if total2xx != totals.count2xx || total4xx != totals.count4xx || total5xx != totals.count5xx {
			t.Fatalf("TimeRange totals mismatch: got=%d/%d/%d want=%d/%d/%d",
				total2xx, total4xx, total5xx, totals.count2xx, totals.count4xx, totals.count5xx)
		}
	})
}

type restTotals struct {
	count2xx int64
	count4xx int64
	count5xx int64
}

func buildRestAggregates(records []logtool.MatchedRecord) (map[string]apistat.ApiLatencyStat, restTotals) {
	type inRecord struct {
		at     time.Time
		method string
		uri    string
	}
	type outRecord struct {
		at     time.Time
		status int
	}

	inMap := make(map[string]inRecord)
	outMap := make(map[string]outRecord)

	for _, item := range records {
		rec := item.Record
		if rec == nil {
			continue
		}
		trace := strings.TrimSpace(rec.TraceID)
		if trace == "" {
			continue
		}
		msg := strings.ToUpper(strings.TrimSpace(rec.Msg))
		switch msg {
		case "IN":
			tm := parseLogTime(rec.Time)
			if tm.IsZero() {
				continue
			}
			method := strings.ToUpper(strings.TrimSpace(rec.Data["method"]))
			uri := normalizeURI(rec.Data["uri"])
			if method == "" || uri == "" {
				continue
			}
			inMap[trace] = inRecord{at: tm, method: method, uri: uri}
		case "OUT":
			tm := parseLogTime(rec.Time)
			if tm.IsZero() {
				continue
			}
			status := parseStatus(rec.Data["status"])
			outMap[trace] = outRecord{at: tm, status: status}
		default:
			continue
		}
	}

	agg := make(map[string]apistat.ApiLatencyStat)
	totals := restTotals{}
	for trace, in := range inMap {
		out, ok := outMap[trace]
		if !ok || in.at.IsZero() || out.at.IsZero() {
			continue
		}
		latency := out.at.Sub(in.at).Milliseconds()
		if latency < 0 {
			continue
		}
		key := in.method + "|" + in.uri
		stat, ok := agg[key]
		if !ok {
			stat = apistat.ApiLatencyStat{
				Method: in.method,
				URI:    in.uri,
			}
		}
		stat.Count++
		stat.SumLatency += latency
		if latency > stat.MaxLatency {
			stat.MaxLatency = latency
		}

		switch {
		case out.status >= 200 && out.status < 300:
			stat.Count2xx++
			stat.SumLatency2xx += latency
			if latency > stat.MaxLatency2xx {
				stat.MaxLatency2xx = latency
			}
			totals.count2xx++
		case out.status >= 400 && out.status < 500:
			stat.Count4xx++
			stat.SumLatency4xx += latency
			if latency > stat.MaxLatency4xx {
				stat.MaxLatency4xx = latency
			}
			totals.count4xx++
		case out.status >= 500 && out.status < 600:
			stat.Count5xx++
			stat.SumLatency5xx += latency
			if latency > stat.MaxLatency5xx {
				stat.MaxLatency5xx = latency
			}
			totals.count5xx++
		default:
			stat.CountOther++
			stat.SumLatencyOther += latency
			if latency > stat.MaxLatencyOther {
				stat.MaxLatencyOther = latency
			}
		}

		agg[key] = stat
	}

	return agg, totals
}

func parseLogTime(val string) time.Time {
	val = strings.TrimSpace(val)
	if val == "" {
		return time.Time{}
	}
	layouts := []string{
		"2006-01-02 15:04:05.000",
		"2006-01-02 15:04:05",
		time.RFC3339Nano,
	}
	for _, l := range layouts {
		if t, err := time.ParseInLocation(l, val, time.Local); err == nil {
			return t
		}
	}
	return time.Time{}
}

func parseStatus(val string) int {
	if val == "" {
		return 0
	}
	if i, err := strconv.Atoi(val); err == nil {
		return i
	}
	return 0
}

func normalizeURI(uri string) string {
	if uri == "" {
		return ""
	}
	if idx := strings.Index(uri, "?"); idx >= 0 {
		return uri[:idx]
	}
	return uri
}

func readLastLogTime(path string) (time.Time, error) {
	f, err := os.Open(path)
	if err != nil {
		return time.Time{}, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return time.Time{}, err
	}
	if info.Size() == 0 {
		return time.Time{}, errors.New("empty log file")
	}

	const readSize = 4096
	buf := make([]byte, readSize)
	offset := int64(0)
	for {
		offset += readSize
		if offset > info.Size() {
			offset = info.Size()
		}
		if _, err := f.Seek(-offset, io.SeekEnd); err != nil {
			return time.Time{}, err
		}
		n, err := f.Read(buf)
		if err != nil && !errors.Is(err, io.EOF) {
			return time.Time{}, err
		}
		data := buf[:n]
		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		var lines []string
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line != "" {
				lines = append(lines, line)
			}
		}
		for i := len(lines) - 1; i >= 0; i-- {
			var payload map[string]any
			if err := json.Unmarshal([]byte(lines[i]), &payload); err != nil {
				continue
			}
			if val, ok := payload["time"].(string); ok {
				tm := parseLogTime(val)
				if !tm.IsZero() {
					return tm, nil
				}
			}
		}
		if offset >= info.Size() {
			break
		}
	}
	return time.Time{}, errors.New("no valid time found")
}

func getApiLatencyStorage(t *testing.T, s *apistat.Serv) cache.Pager[apistat.ApiLatencyStat] {
	t.Helper()
	val := reflect.ValueOf(s).Elem()
	field := val.FieldByName("storage")
	if !field.IsValid() {
		t.Fatalf("storage field not found")
	}
	ptr := unsafe.Pointer(field.UnsafeAddr())
	storage := reflect.NewAt(field.Type(), ptr).Elem().Interface()
	pager, ok := storage.(cache.Pager[apistat.ApiLatencyStat])
	if !ok {
		t.Fatalf("storage has unexpected type: %T", storage)
	}
	return pager
}
