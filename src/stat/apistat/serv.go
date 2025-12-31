package apistat

import (
	"context"
	"fmt"
	"github.com/jom-io/gorig-om/src/logtool"
	"github.com/jom-io/gorig/cache"
	"github.com/jom-io/gorig/cronx"
	"github.com/jom-io/gorig/utils/errors"
	"github.com/jom-io/gorig/utils/logger"
	"go.uber.org/zap"
	"strconv"
	"strings"
	"time"
)

var (
	statServ  *Serv
	latMaxAge = 30 * 24 * time.Hour
)

type Serv struct {
	storage cache.Pager[ApiLatencyStat]
	meta    cache.Pager[ApiLatencyMeta]
}

func S() *Serv {
	if statServ == nil {
		statServ = &Serv{
			storage: cache.NewPager[ApiLatencyStat](context.Background(), cache.Sqlite, "api_latency_stat"),
			meta:    cache.NewPager[ApiLatencyMeta](context.Background(), cache.Sqlite, "api_latency_meta"),
		}
	}
	return statServ
}

func init() {
	cronx.AddCronTask("45 * * * * *", S().Collect, 30*time.Second)

	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			if err := S().Clear(context.Background()); err != nil {
				logger.Error(context.Background(), "clear api latency failed", zap.Error(err))
			}
		}
	}()
}

func (s *Serv) Collect(ctx context.Context) {
	opts := logtool.SearchOptions{
		StartTime: time.Now().Add(-time.Minute).Format(time.DateTime),
		EndTime:   time.Now().Format(time.DateTime),
		Levels:    []string{logtool.InfoLevel.Str(), logtool.WarnLevel.Str(), logtool.ErrorLevel.Str()},
	}
	logs, e := logtool.SearchLogs(opts)
	if e != nil {
		logger.Error(ctx, "latency collect search failed", zap.Error(e))
		return
	}

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

	for _, item := range logs {
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
			tm := parseTime(rec.Time)
			method := strings.ToUpper(strings.TrimSpace(rec.Data["method"]))
			uri := rec.Data["uri"]
			inMap[trace] = inRecord{at: tm, method: method, uri: uri}
		case "OUT":
			tm := parseTime(rec.Time)
			status := parseStatus(rec.Data["status"])
			outMap[trace] = outRecord{at: tm, status: status}
		default:
			continue
		}
	}

	agg := make(map[string]*ApiLatencyStat)
	nowBucket := time.Now().Truncate(time.Minute).Unix()
	metaSamples := make(map[string]*ApiLatencyMeta)

	for trace, in := range inMap {
		out, ok := outMap[trace]
		if !ok || in.at.IsZero() || out.at.IsZero() {
			continue
		}
		latency := out.at.Sub(in.at).Milliseconds()
		if latency < 0 {
			continue
		}
		key := fmt.Sprintf("%s|%s", in.method, in.uri)
		stat, ok := agg[key]
		if !ok {
			stat = &ApiLatencyStat{
				At:     nowBucket,
				Method: in.method,
				URI:    in.uri,
			}
			agg[key] = stat
		}
		stat.Count++
		stat.SumLatency += latency
		if latency > stat.MaxLatency {
			stat.MaxLatency = latency
		}

		status := out.status
		switch {
		case status >= 200 && status < 300:
			stat.Count2xx++
			stat.SumLatency2xx += latency
			if latency > stat.MaxLatency2xx {
				stat.MaxLatency2xx = latency
			}
		case status >= 400 && status < 500:
			stat.Count4xx++
			stat.SumLatency4xx += latency
			if latency > stat.MaxLatency4xx {
				stat.MaxLatency4xx = latency
			}
		case status >= 500 && status < 600:
			stat.Count5xx++
			stat.SumLatency5xx += latency
			if latency > stat.MaxLatency5xx {
				stat.MaxLatency5xx = latency
			}
		default:
			stat.CountOther++
			stat.SumLatencyOther += latency
			if latency > stat.MaxLatencyOther {
				stat.MaxLatencyOther = latency
			}
		}

		if _, ok := metaSamples[key]; !ok {
			metaSamples[key] = &ApiLatencyMeta{
				Method:       in.method,
				URI:          in.uri,
				SampleTrace:  trace,
				SampleStatus: status,
				FirstAt:      nowBucket,
				LastAt:       nowBucket,
			}
		}
	}

	for key, stat := range agg {
		if err := s.storage.Put(*stat); err != nil {
			logger.Error(ctx, "save api latency stat failed", zap.Error(err), zap.String("key", key))
		}
	}

	for key, meta := range metaSamples {
		cond := map[string]any{
			"method": meta.Method,
			"uri":    meta.URI,
		}
		old, err := s.meta.Get(cond)
		if err != nil {
			logger.Error(ctx, "get api latency meta failed", zap.Error(err), zap.String("key", key))
			continue
		}
		if old == nil {
			if err := s.meta.Put(*meta); err != nil {
				logger.Error(ctx, "save api latency meta failed", zap.Error(err), zap.String("key", key))
			}
		} else {
			updated := *old
			updated.LastAt = nowBucket
			if updated.SampleTrace == "" && meta.SampleTrace != "" {
				updated.SampleTrace = meta.SampleTrace
				updated.SampleStatus = meta.SampleStatus
			}
			_ = s.meta.Update(cond, &updated)
		}
	}
}

func (s *Serv) Clear(ctx context.Context) error {
	expiration := time.Now().Add(-latMaxAge).Unix()
	if err := s.storage.Delete(map[string]any{"at": map[string]any{"$lt": expiration}}); err != nil {
		return err
	}
	if err := s.meta.Delete(map[string]any{"lastAt": map[string]any{"$lt": expiration}}); err != nil {
		return err
	}
	return nil
}

func (s *Serv) Top(ctx context.Context, start, end int64, methods []string, uriPrefix string, statuses []string, sortBy string, asc bool, limit int64) ([]*ApiLatencyRank, *errors.Error) {
	if start == 0 || end == 0 || start > end {
		return nil, errors.Verify("invalid time range")
	}
	if limit <= 0 {
		limit = 10
	}
	cond := map[string]any{
		"at": map[string]any{
			"$gte": start,
			"$lte": end,
		},
	}
	if len(methods) == 1 {
		cond["method"] = strings.ToUpper(methods[0])
	} else if len(methods) > 1 {
		var vals []string
		for _, m := range methods {
			if strings.TrimSpace(m) == "" {
				continue
			}
			vals = append(vals, strings.ToUpper(m))
		}
		if len(vals) > 0 {
			cond["method"] = map[string]any{"$in": vals}
		}
	}
	if strings.TrimSpace(uriPrefix) != "" {
		cond["uri"] = map[string]any{"$like": uriPrefix + "%"}
	}

	groupFields := []string{"method", "uri"}
	aggFields := []cache.AggField{
		{Field: "count", Agg: cache.AggSum, Alias: "cnt"},
		{Field: "sumLatency", Agg: cache.AggSum, Alias: "sum"},
		{Field: "maxLatency", Agg: cache.AggMax, Alias: "max"},
		{Field: "count2xx", Agg: cache.AggSum, Alias: "c2"},
		{Field: "sumLatency2xx", Agg: cache.AggSum, Alias: "s2"},
		{Field: "maxLatency2xx", Agg: cache.AggMax, Alias: "m2"},
		{Field: "count4xx", Agg: cache.AggSum, Alias: "c4"},
		{Field: "sumLatency4xx", Agg: cache.AggSum, Alias: "s4"},
		{Field: "maxLatency4xx", Agg: cache.AggMax, Alias: "m4"},
		{Field: "count5xx", Agg: cache.AggSum, Alias: "c5"},
		{Field: "sumLatency5xx", Agg: cache.AggSum, Alias: "s5"},
		{Field: "maxLatency5xx", Agg: cache.AggMax, Alias: "m5"},
		{Field: "countOther", Agg: cache.AggSum, Alias: "co"},
		{Field: "sumLatencyOther", Agg: cache.AggSum, Alias: "so"},
		{Field: "maxLatencyOther", Agg: cache.AggMax, Alias: "mo"},
	}

	orderExpr, errSort := buildLatencyOrder(sortBy)
	if errSort != nil {
		return nil, errSort
	}

	grouped, err := s.storage.GroupByFields(cond, groupFields, aggFields, limit, cache.PageSorter{SortField: orderExpr.field, Expr: orderExpr.expr, Asc: asc})
	if err != nil {
		logger.Error(ctx, "GroupByFields latency failed", zap.Error(err))
		return nil, errors.Sys("GroupByFields latency failed", err)
	}

	useStatus := len(statuses) > 0
	statusSet := make(map[string]struct{})
	for _, st := range statuses {
		if strings.TrimSpace(st) == "" {
			continue
		}
		statusSet[strings.ToLower(st)] = struct{}{}
	}

	result := make([]*ApiLatencyRank, 0, len(grouped))
	for _, item := range grouped {
		cnt := int64(item.Value["cnt"])
		sum := int64(item.Value["sum"])
		maxVal := int64(item.Value["max"])

		if useStatus {
			cnt = 0
			sum = 0
			maxVal = 0
			if _, ok := statusSet["2xx"]; ok {
				cnt += int64(item.Value["c2"])
				sum += int64(item.Value["s2"])
				if int64(item.Value["m2"]) > maxVal {
					maxVal = int64(item.Value["m2"])
				}
			}
			if _, ok := statusSet["4xx"]; ok {
				cnt += int64(item.Value["c4"])
				sum += int64(item.Value["s4"])
				if int64(item.Value["m4"]) > maxVal {
					maxVal = int64(item.Value["m4"])
				}
			}
			if _, ok := statusSet["5xx"]; ok {
				cnt += int64(item.Value["c5"])
				sum += int64(item.Value["s5"])
				if int64(item.Value["m5"]) > maxVal {
					maxVal = int64(item.Value["m5"])
				}
			}
			if _, ok := statusSet["other"]; ok {
				cnt += int64(item.Value["co"])
				sum += int64(item.Value["so"])
				if int64(item.Value["mo"]) > maxVal {
					maxVal = int64(item.Value["mo"])
				}
			}
		}

		if cnt == 0 {
			continue
		}
		avg := sum / cnt
		r := &ApiLatencyRank{
			Method:     item.Group["method"],
			URI:        item.Group["uri"],
			Count:      int64(item.Value["cnt"]),
			AvgLatency: avg,
			MaxLatency: maxVal,
			Count2xx:   int64(item.Value["c2"]),
			Count4xx:   int64(item.Value["c4"]),
			Count5xx:   int64(item.Value["c5"]),
			CountOther: int64(item.Value["co"]),
		}
		result = append(result, r)
	}

	for _, r := range result {
		meta, _ := s.meta.Get(map[string]any{
			"method": r.Method,
			"uri":    r.URI,
		})
		if meta != nil && r.SampleTrace == "" {
			r.SampleTrace = meta.SampleTrace
		}
	}

	return result, nil
}

type orderExpr struct {
	field string
	expr  string
}

func buildLatencyOrder(sortBy string) (orderExpr, *errors.Error) {
	sortBy = strings.ToLower(strings.TrimSpace(sortBy))
	if sortBy == "" {
		sortBy = "avg"
	}
	switch sortBy {
	case "avg":
		return orderExpr{field: "avg", expr: "sum / NULLIF(cnt,0)"}, nil
	case "max":
		return orderExpr{field: "max"}, nil
	case "count", "cnt":
		return orderExpr{field: "cnt"}, nil
	case "2xx":
		return orderExpr{field: "c2"}, nil
	case "4xx":
		return orderExpr{field: "c4"}, nil
	case "5xx":
		return orderExpr{field: "c5"}, nil
	case "other":
		return orderExpr{field: "co"}, nil
	case "p95":
		return orderExpr{}, errors.Verify("p95 sorting is not supported yet")
	default:
		return orderExpr{}, errors.Verify(fmt.Sprintf("unsupported sortBy: %s", sortBy))
	}
}

func parseTime(val string) time.Time {
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
