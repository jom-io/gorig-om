package test

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
	"unsafe"

	"github.com/jom-io/gorig-om/src/stat/apistat"
	"github.com/jom-io/gorig/cache"
)

type apiTestReq struct {
	method    string
	uri       string
	status    int
	latencyMs int64
}

func TestApiStatWorkflow(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir failed: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(origDir)
	})
	if err := os.MkdirAll(".logs", 0755); err != nil {
		t.Fatalf("mkdir .logs failed: %v", err)
	}

	ctx := context.Background()
	s := apistat.S()
	storage := getApiLatencyStorage(t, s)
	_ = storage.Delete(map[string]any{
		"uri": map[string]any{"$like": "/__apistat_test__%"},
	})

	requests := []apiTestReq{
		{method: "GET", uri: "/__apistat_test__/alpha", status: 200, latencyMs: 120},
		{method: "GET", uri: "/__apistat_test__/alpha", status: 500, latencyMs: 90},
		{method: "GET", uri: "/__apistat_test__/beta", status: 404, latencyMs: 130},
		{method: "POST", uri: "/__apistat_test__/gamma", status: 200, latencyMs: 250},
		{method: "POST", uri: "/__apistat_test__/gamma", status: 200, latencyMs: 150},
	}
	slowThreshold := int64(200)

	nowBucket := time.Now().Truncate(time.Minute).Unix()
	stats := make(map[string]*apistat.ApiLatencyStat)
	for i, req := range requests {
		_ = i
		key := req.method + "|" + req.uri
		stat, ok := stats[key]
		if !ok {
			stat = &apistat.ApiLatencyStat{
				At:     nowBucket,
				Method: req.method,
				URI:    req.uri,
			}
			stats[key] = stat
		}

		stat.Count++
		if req.latencyMs > slowThreshold {
			stat.CountSlow++
		}
		stat.SumLatency += req.latencyMs
		if req.latencyMs > stat.MaxLatency {
			stat.MaxLatency = req.latencyMs
		}

		switch {
		case req.status >= 200 && req.status < 300:
			stat.Count2xx++
			stat.SumLatency2xx += req.latencyMs
			if req.latencyMs > stat.MaxLatency2xx {
				stat.MaxLatency2xx = req.latencyMs
			}
		case req.status >= 400 && req.status < 500:
			stat.Count4xx++
			stat.SumLatency4xx += req.latencyMs
			if req.latencyMs > stat.MaxLatency4xx {
				stat.MaxLatency4xx = req.latencyMs
			}
		case req.status >= 500 && req.status < 600:
			stat.Count5xx++
			stat.SumLatency5xx += req.latencyMs
			if req.latencyMs > stat.MaxLatency5xx {
				stat.MaxLatency5xx = req.latencyMs
			}
		default:
			stat.CountOther++
			stat.SumLatencyOther += req.latencyMs
			if req.latencyMs > stat.MaxLatencyOther {
				stat.MaxLatencyOther = req.latencyMs
			}
		}
	}

	for _, stat := range stats {
		if err := storage.Put(*stat); err != nil {
			t.Fatalf("storage put failed: %v", err)
		}
	}

	start := time.Now().Add(-5 * time.Minute).Unix()
	end := time.Now().Unix()

	t.Run("TopPage", func(t *testing.T) {
		page, err := s.TopPage(ctx, start, end, 1, 10, nil, nil, "/__apistat_test__", "", nil, "success", false)
		if err != nil {
			t.Fatalf("TopPage failed: %v", err)
		}
		if page == nil || len(page.Items) == 0 {
			t.Fatalf("TopPage returned empty result")
		}

		expected := map[string]struct {
			count    int64
			count2xx int64
			count4xx int64
			count5xx int64
			success  float64
		}{
			"/__apistat_test__/alpha": {count: 2, count2xx: 1, count4xx: 0, count5xx: 1, success: 0.5},
			"/__apistat_test__/beta":  {count: 1, count2xx: 0, count4xx: 1, count5xx: 0, success: 0.0},
			"/__apistat_test__/gamma": {count: 2, count2xx: 2, count4xx: 0, count5xx: 0, success: 1.0},
		}

		for i := 1; i < len(page.Items); i++ {
			if page.Items[i-1].SuccessRate < page.Items[i].SuccessRate {
				t.Fatalf("TopPage not sorted by successRate desc: %v < %v", page.Items[i-1].SuccessRate, page.Items[i].SuccessRate)
			}
		}

		seen := map[string]bool{}
		for _, item := range page.Items {
			exp, ok := expected[item.URI]
			if !ok {
				continue
			}
			seen[item.URI] = true
			if item.Count != exp.count || item.Count2xx != exp.count2xx || item.Count4xx != exp.count4xx || item.Count5xx != exp.count5xx {
				t.Fatalf("unexpected counts for %s: got=%d/%d/%d/%d", item.URI, item.Count, item.Count2xx, item.Count4xx, item.Count5xx)
			}
			if math.Abs(item.SuccessRate-exp.success) > 0.0001 {
				t.Fatalf("unexpected successRate for %s: got=%v want=%v", item.URI, item.SuccessRate, exp.success)
			}
		}
		for uri := range expected {
			if !seen[uri] {
				t.Fatalf("TopPage missing expected uri: %s", uri)
			}
		}
	})

	t.Run("TopPageStatusFilter", func(t *testing.T) {
		page, err := s.TopPage(ctx, start, end, 1, 10, nil, nil, "/__apistat_test__", "", []string{"5xx"}, "count", false)
		if err != nil {
			t.Fatalf("TopPage status filter failed: %v", err)
		}
		if page == nil || len(page.Items) != 1 {
			t.Fatalf("expected 1 item for 5xx filter, got %v", page)
		}
		if page.Items[0].URI != "/__apistat_test__/alpha" {
			t.Fatalf("unexpected 5xx item uri: %s", page.Items[0].URI)
		}
	})

	t.Run("Summary", func(t *testing.T) {
		sum, err := s.Summary(ctx, start, end, 200)
		if err != nil {
			t.Fatalf("Summary failed: %v", err)
		}
		if sum.Count < 5 {
			t.Fatalf("Summary count too small: %d", sum.Count)
		}
		if sum.Count5xx < 1 {
			t.Fatalf("Summary 5xx too small: %d", sum.Count5xx)
		}
		if sum.SlowCount != 1 {
			t.Fatalf("Summary slowCount expected 1, got: %d", sum.SlowCount)
		}
	})

	t.Run("TimeRange", func(t *testing.T) {
		items, err := s.TimeRange(ctx, start, end, cache.GranularityMinute, apistat.ApiStatCount2xx, apistat.ApiStatCount4xx, apistat.ApiStatCount5xx)
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
		if total2xx < 3 || total4xx < 1 || total5xx < 1 {
			t.Fatalf("TimeRange totals too small: 2xx=%d 4xx=%d 5xx=%d", total2xx, total4xx, total5xx)
		}
	})

	t.Run("InvalidTimeRange", func(t *testing.T) {
		_, err := s.TimeRange(ctx, end, start, cache.GranularityMinute, apistat.ApiStatCount2xx)
		if err == nil {
			t.Fatalf("expected invalid time range error")
		}
	})
}

func TestApiStatCollectCounts4xx(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir failed: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(origDir)
	})

	logDir := filepath.Join(".", ".logs", "rest")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		t.Fatalf("mkdir log dir failed: %v", err)
	}

	baseTime := time.Now().Add(-10 * time.Second)
	logFile := filepath.Join(logDir, "rest-"+baseTime.Format("2006-01-02T15-04-05.000")+".jsonl")
	f, err := os.Create(logFile)
	if err != nil {
		t.Fatalf("create log file failed: %v", err)
	}
	defer func() {
		_ = f.Close()
		_ = os.Remove(logFile)
	}()

	traceID := "apistat_test_404"
	inTime := baseTime
	outTime := baseTime.Add(50 * time.Millisecond)
	if err := writeJSONLine(f, map[string]any{
		"time":       inTime.Format("2006-01-02 15:04:05.000"),
		"level":      "info",
		"msg":        "IN",
		"_trace_id_": traceID,
		"method":     "GET",
		"uri":        "/__apistat_test__/not_found",
	}); err != nil {
		t.Fatalf("write IN log failed: %v", err)
	}
	if err := writeJSONLine(f, map[string]any{
		"time":       outTime.Format("2006-01-02 15:04:05.000"),
		"level":      "info",
		"msg":        "OUT",
		"_trace_id_": traceID,
		"status":     404,
	}); err != nil {
		t.Fatalf("write OUT log failed: %v", err)
	}
	if err := f.Sync(); err != nil {
		t.Fatalf("sync log file failed: %v", err)
	}

	s := newApiStatServ(t, "api_latency_stat_collect_404")
	s.Collect(context.Background())

	start := time.Now().Add(-1 * time.Minute).Unix()
	end := time.Now().Unix()
	page, topErr := s.TopPage(context.Background(), start, end, 1, 10, nil, nil, "/__apistat_test__", "", []string{"4xx"}, "count", false)
	if topErr != nil {
		t.Fatalf("TopPage failed: %v", topErr)
	}
	if page.Total < 1 || len(page.Items) == 0 {
		t.Fatalf("expected 4xx item, got empty result")
	}
	item := page.Items[0]
	if item.Count4xx < 1 {
		t.Fatalf("expected 4xx count >=1, got %d", item.Count4xx)
	}
}

func TestApiStatSample(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir failed: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(origDir)
	})

	logDir := filepath.Join(".", ".logs", "rest")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		t.Fatalf("mkdir log dir failed: %v", err)
	}

	baseTime := time.Now().Add(-20 * time.Second)
	logFile := filepath.Join(logDir, "rest-"+baseTime.Format("2006-01-02T15-04-05.000")+".jsonl")
	f, err := os.Create(logFile)
	if err != nil {
		t.Fatalf("create log file failed: %v", err)
	}
	defer func() {
		_ = f.Close()
		_ = os.Remove(logFile)
	}()

	writePair := func(traceID, method, uri string, status int, inTime time.Time, latencyMs int64) {
		outTime := inTime.Add(time.Duration(latencyMs) * time.Millisecond)
		if err := writeJSONLine(f, map[string]any{
			"time":       inTime.Format("2006-01-02 15:04:05.000"),
			"level":      "info",
			"msg":        "IN",
			"_trace_id_": traceID,
			"method":     method,
			"uri":        uri,
			"foo":        "bar",
		}); err != nil {
			t.Fatalf("write IN log failed: %v", err)
		}
		if err := writeJSONLine(f, map[string]any{
			"time":       outTime.Format("2006-01-02 15:04:05.000"),
			"level":      "info",
			"msg":        "OUT",
			"_trace_id_": traceID,
			"status":     status,
			"error":      "boom",
		}); err != nil {
			t.Fatalf("write OUT log failed: %v", err)
		}
	}

	method := "GET"
	uriPrefix := "/__apistat_sample__/demo"
	writePair("trace_2xx_old", method, uriPrefix+"?x=0", 200, baseTime.Add(1*time.Second), 120)
	writePair("trace_2xx_new", method, uriPrefix+"?x=1", 200, baseTime.Add(5*time.Second), 80)
	writePair("trace_4xx", method, uriPrefix+"?x=2", 404, baseTime.Add(6*time.Second), 200)
	writePair("trace_5xx_slow", method, uriPrefix+"?x=3", 500, baseTime.Add(7*time.Second), 900)
	writePair("trace_5xx_new", method, uriPrefix+"?x=4", 500, baseTime.Add(9*time.Second), 100)

	if err := f.Sync(); err != nil {
		t.Fatalf("sync log file failed: %v", err)
	}

	s := newApiStatServ(t, "api_latency_stat_sample")
	s.Collect(context.Background())

	resp, sampleErr := s.Sample(context.Background(), method, uriPrefix+"?x=1", nil)
	if sampleErr != nil {
		t.Fatalf("Sample failed: %v", sampleErr)
	}
	if resp.Sample2xx == nil || resp.Sample2xx.TraceID != "trace_2xx_new" {
		t.Fatalf("sample2xx mismatch: %+v", resp.Sample2xx)
	}
	if resp.Sample4xx == nil || resp.Sample4xx.TraceID != "trace_4xx" {
		t.Fatalf("sample4xx mismatch: %+v", resp.Sample4xx)
	}
	if resp.Sample5xx == nil || resp.Sample5xx.TraceID != "trace_5xx_new" {
		t.Fatalf("sample5xx mismatch: %+v", resp.Sample5xx)
	}
	if resp.SampleSlow == nil || resp.SampleSlow.TraceID != "trace_5xx_slow" {
		t.Fatalf("sampleSlow mismatch: %+v", resp.SampleSlow)
	}
	if resp.Latest == nil || resp.Latest.TraceID != "trace_5xx_new" {
		t.Fatalf("latest mismatch: %+v", resp.Latest)
	}
	if resp.Sample2xx.URL != uriPrefix+"?x=1" {
		t.Fatalf("sample2xx url mismatch: %s", resp.Sample2xx.URL)
	}
	if resp.Sample2xx.InLog.Msg != "IN" || resp.Sample2xx.OutLog.Msg != "OUT" {
		t.Fatalf("sample2xx log missing msg: %+v", resp.Sample2xx)
	}
	if resp.Sample2xx.InLog.Data["uri"] != uriPrefix+"?x=1" {
		t.Fatalf("sample2xx log uri mismatch: %+v", resp.Sample2xx.InLog.Data)
	}

	filtered, filterErr := s.Sample(context.Background(), method, uriPrefix+"?x=1", []string{"slow"})
	if filterErr != nil {
		t.Fatalf("Sample slow filter failed: %v", filterErr)
	}
	if filtered.SampleSlow == nil || filtered.SampleSlow.TraceID != "trace_5xx_slow" {
		t.Fatalf("filtered slow sample mismatch: %+v", filtered.SampleSlow)
	}
	if filtered.Sample2xx != nil || filtered.Sample4xx != nil || filtered.Sample5xx != nil || filtered.Latest != nil {
		t.Fatalf("filtered response should only include slow sample")
	}
}

func newApiStatServ(t *testing.T, storageName string) *apistat.Serv {
	t.Helper()
	s := &apistat.Serv{}
	storage := cache.NewPager[apistat.ApiLatencyStat](context.Background(), cache.Sqlite, storageName)
	meta := cache.NewPager[apistat.ApiLatencyMeta](context.Background(), cache.Sqlite, storageName+"_meta")

	val := reflect.ValueOf(s).Elem()
	setUnexportedField(val.FieldByName("storage"), storage)
	setUnexportedField(val.FieldByName("meta"), meta)
	return s
}

func setUnexportedField(field reflect.Value, value interface{}) {
	ptr := unsafe.Pointer(field.UnsafeAddr())
	reflect.NewAt(field.Type(), ptr).Elem().Set(reflect.ValueOf(value))
}

func writeJSONLine(f *os.File, data map[string]any) error {
	line, err := json.Marshal(data)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintln(f, string(line)); err != nil {
		return err
	}
	return nil
}
