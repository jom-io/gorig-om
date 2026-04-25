package apistat

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jom-io/gorig-om/src/logtool"
	"github.com/jom-io/gorig/cache"
	"github.com/jom-io/gorig/cronx"
	"github.com/jom-io/gorig/utils/errors"
	"github.com/jom-io/gorig/utils/logger"
	"go.uber.org/zap"
)

var (
	statServ  *Serv
	latMaxAge = 30 * 24 * time.Hour
)

const (
	defaultSlowMs    int64 = 200
	logSearchMaxSize       = 50000
)

var summaryExcludeMethods = []string{"OPTIONS", "HEAD", "PATCH"}

type Serv struct {
	storage     cache.Pager[ApiLatencyStat]
	storageHour cache.Pager[ApiLatencyStat]
	meta        cache.Pager[ApiLatencyMeta]
	rollupMu    sync.Mutex
	lastRollup  int64
}

func S() *Serv {
	if statServ == nil {
		statServ = &Serv{
			storage:     cache.NewPager[ApiLatencyStat](context.Background(), cache.Sqlite, "api_latency_stat"),
			storageHour: cache.NewPager[ApiLatencyStat](context.Background(), cache.Sqlite, "api_latency_stat_hour"),
			meta:        cache.NewPager[ApiLatencyMeta](context.Background(), cache.Sqlite, "api_latency_meta"),
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
		Categories: []string{
			"rest",
			"invoke",
		},
		Levels: []string{logtool.InfoLevel.Str(), logtool.WarnLevel.Str(), logtool.ErrorLevel.Str()},
		Size:   logSearchMaxSize,
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
		log    ApiLogSample
	}
	type outRecord struct {
		at     time.Time
		status int
		log    ApiLogSample
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
			inMap[trace] = inRecord{at: tm, method: method, uri: uri, log: buildLogSample(rec)}
		case "OUT":
			tm := parseTime(rec.Time)
			status := parseStatus(rec.Data["status"])
			outMap[trace] = outRecord{at: tm, status: status, log: buildLogSample(rec)}
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
		normalizedURI := normalizeURI(in.uri)
		key := fmt.Sprintf("%s|%s", in.method, normalizedURI)
		stat, ok := agg[key]
		if !ok {
			stat = &ApiLatencyStat{
				At:     nowBucket,
				Method: in.method,
				URI:    normalizedURI,
			}
			agg[key] = stat
		}
		stat.Count++
		if latency > defaultSlowMs {
			stat.CountSlow++
		}
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

		sample := &ApiLatencySample{
			TraceID:   trace,
			URL:       in.uri,
			RequestAt: in.at.UnixMilli(),
			Status:    status,
			LatencyMs: latency,
			InLog:     in.log,
			OutLog:    out.log,
		}

		meta, ok := metaSamples[key]
		if !ok {
			meta = &ApiLatencyMeta{
				Method:  in.method,
				URI:     normalizedURI,
				FirstAt: nowBucket,
				LastAt:  nowBucket,
			}
		}
		updateSampleLatest(meta, sample)
		updateSampleSlow(meta, sample)
		switch {
		case status >= 200 && status < 300:
			updateSampleByTime(&meta.Sample2xx, sample)
		case status >= 400 && status < 500:
			updateSampleByTime(&meta.Sample4xx, sample)
		case status >= 500 && status < 600:
			updateSampleByTime(&meta.Sample5xx, sample)
		}
		metaSamples[key] = meta
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
			if updated.FirstAt == 0 {
				updated.FirstAt = meta.FirstAt
			}
			mergeSampleLatest(&updated, meta.SampleLatest)
			mergeSampleSlow(&updated, meta.SampleSlow)
			mergeSampleByTime(&updated.Sample2xx, meta.Sample2xx)
			mergeSampleByTime(&updated.Sample4xx, meta.Sample4xx)
			mergeSampleByTime(&updated.Sample5xx, meta.Sample5xx)
			_ = s.meta.Update(cond, &updated)
		}
	}
}

func (s *Serv) Clear(ctx context.Context) error {
	now := time.Now()
	currentHourStart := now.Truncate(time.Hour).Unix()

	if s.storageHour != nil {
		s.rollupMu.Lock()
		if s.lastRollup != currentHourStart {
			startHour, endHour, ok, err := s.rollupWindow(currentHourStart)
			if err != nil {
				s.rollupMu.Unlock()
				return err
			}
			if ok {
				if err := s.rollupHours(ctx, startHour, endHour); err != nil {
					s.rollupMu.Unlock()
					return err
				}
			}
			if err := s.storage.Delete(map[string]any{"at": map[string]any{"$lt": currentHourStart}}); err != nil {
				s.rollupMu.Unlock()
				return err
			}
			s.lastRollup = currentHourStart
		}
		s.rollupMu.Unlock()
	} else {
		if err := s.storage.Delete(map[string]any{"at": map[string]any{"$lt": currentHourStart}}); err != nil {
			return err
		}
	}

	expiration := now.Add(-latMaxAge).Unix()
	if s.storageHour != nil {
		if err := s.storageHour.Delete(map[string]any{"at": map[string]any{"$lt": expiration}}); err != nil {
			return err
		}
	}
	if err := s.meta.Delete(map[string]any{"lastAt": map[string]any{"$lt": expiration}}); err != nil {
		return err
	}
	return nil
}

func (s *Serv) TimeRange(ctx context.Context, start, end int64, granularity cache.Granularity, field ...ApiStatType) ([]*cache.PageTimeItem, *errors.Error) {
	from := time.Unix(start, 0)
	to := time.Unix(end, 0)
	if from.IsZero() || to.IsZero() || from.After(to) {
		return nil, errors.Verify("Invalid time range")
	}
	if granularity == "" {
		granularity = cache.GranularityHour
	}
	fields := make([]string, 0, len(field))
	include2xx := false
	for _, f := range field {
		fields = append(fields, f.String())
		if f == ApiStatCount2xx {
			include2xx = true
		}
	}
	if len(fields) == 0 {
		fields = []string{ApiStatCount2xx.String(), ApiStatCount4xx.String(), ApiStatCount5xx.String()}
		include2xx = true
	}

	cond := map[string]any{}
	if include2xx && len(summaryExcludeMethods) > 0 {
		cond["method"] = map[string]any{"$nin": summaryExcludeMethods}
	}

	result, err := s.storage.GroupByTime(cond, from, to, granularity, cache.AggSum, fields...)
	if err != nil {
		logger.Error(ctx, "GroupByTime failed", zap.Error(err))
		return nil, errors.Sys("GroupByTime failed", err)
	}
	return result, nil
}

func (s *Serv) Summary(ctx context.Context, start, end int64, slowMs int64) (*ApiLatencySummary, *errors.Error) {
	from := time.Unix(start, 0)
	to := time.Unix(end, 0)
	if from.IsZero() || to.IsZero() || from.After(to) {
		return nil, errors.Verify("Invalid time range")
	}
	if slowMs <= 0 {
		slowMs = defaultSlowMs
	}

	cond := map[string]any{}
	if len(summaryExcludeMethods) > 0 {
		cond["method"] = map[string]any{"$nin": summaryExcludeMethods}
	}

	items, err := s.storage.GroupByTime(cond, from, to, cache.GranularityMinute, cache.AggSum,
		ApiStatCount.String(),
		ApiStatSumLatency.String(),
		ApiStatCount5xx.String(),
		ApiStatCountSlow.String(),
		ApiStatCount2xx.String(),
		ApiStatSumLatency2xx.String(),
	)
	if err != nil {
		logger.Error(ctx, "GroupByTime summary failed", zap.Error(err))
		return nil, errors.Sys("GroupByTime summary failed", err)
	}

	var totalCount int64
	var total5xx int64
	var totalSlow int64
	var total2xx int64
	var totalSum2xx int64
	for _, item := range items {
		totalCount += int64(item.Value[ApiStatCount.String()])
		total5xx += int64(item.Value[ApiStatCount5xx.String()])
		totalSlow += int64(item.Value[ApiStatCountSlow.String()])
		total2xx += int64(item.Value[ApiStatCount2xx.String()])
		totalSum2xx += int64(item.Value[ApiStatSumLatency2xx.String()])
	}

	var avg int64
	if total2xx > 0 {
		avg = totalSum2xx / total2xx
	}

	slowCount := totalSlow
	if slowMs != defaultSlowMs {
		var e *errors.Error
		slowCount, e = s.countSlow(ctx, from, to, slowMs, summaryExcludeMethods)
		if e != nil {
			return nil, e
		}
	}

	return &ApiLatencySummary{
		Count:      totalCount,
		AvgLatency: avg,
		Count5xx:   total5xx,
		SlowCount:  slowCount,
		UpdatedAt:  time.Now().Unix(),
	}, nil
}

func latencyGroupFields() []string {
	return []string{"method", "uri"}
}

func latencyAggFields() []cache.AggField {
	return []cache.AggField{
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
}

func (s *Serv) minMaxAt(storage cache.Pager[ApiLatencyStat]) (int64, int64, bool, error) {
	if storage == nil {
		return 0, 0, false, nil
	}
	minPage, err := storage.Find(1, 1, nil, cache.PageSorterAsc("at"))
	if err != nil {
		return 0, 0, false, err
	}
	if minPage == nil || len(minPage.Items) == 0 {
		return 0, 0, false, nil
	}
	maxPage, err := storage.Find(1, 1, nil, cache.PageSorterDesc("at"))
	if err != nil {
		return 0, 0, false, err
	}
	if maxPage == nil || len(maxPage.Items) == 0 {
		return 0, 0, false, nil
	}
	return minPage.Items[0].At, maxPage.Items[0].At, true, nil
}

func (s *Serv) maxAt(storage cache.Pager[ApiLatencyStat]) (int64, bool, error) {
	if storage == nil {
		return 0, false, nil
	}
	maxPage, err := storage.Find(1, 1, nil, cache.PageSorterDesc("at"))
	if err != nil {
		return 0, false, err
	}
	if maxPage == nil || len(maxPage.Items) == 0 {
		return 0, false, nil
	}
	return maxPage.Items[0].At, true, nil
}

type latencyAgg struct {
	Method string
	URI    string
	Cnt    int64
	Sum    int64
	Max    int64
	C2     int64
	C4     int64
	C5     int64
	Co     int64
}

func mergeLatencyItems(dst map[string]*latencyAgg, items []*cache.PageGroupItem) {
	for _, item := range items {
		if item == nil {
			continue
		}
		method := item.Group["method"]
		uri := item.Group["uri"]
		if method == "" || uri == "" {
			continue
		}
		key := method + "|" + uri
		agg, ok := dst[key]
		if !ok {
			agg = &latencyAgg{Method: method, URI: uri}
			dst[key] = agg
		}
		cnt := int64(item.Value["cnt"])
		sum := int64(item.Value["sum"])
		maxv := int64(item.Value["max"])
		agg.Cnt += cnt
		agg.Sum += sum
		if maxv > agg.Max {
			agg.Max = maxv
		}
		agg.C2 += int64(item.Value["c2"])
		agg.C4 += int64(item.Value["c4"])
		agg.C5 += int64(item.Value["c5"])
		agg.Co += int64(item.Value["co"])
	}
}

func buildLatencyRanksFromAgg(aggs map[string]*latencyAgg, statusSet map[string]struct{}) []*ApiLatencyRank {
	result := make([]*ApiLatencyRank, 0, len(aggs))
	for _, agg := range aggs {
		if agg == nil || agg.Cnt == 0 {
			continue
		}
		if len(statusSet) > 0 {
			match := false
			if _, ok := statusSet["2xx"]; ok && agg.C2 > 0 {
				match = true
			}
			if _, ok := statusSet["4xx"]; ok && agg.C4 > 0 {
				match = true
			}
			if _, ok := statusSet["5xx"]; ok && agg.C5 > 0 {
				match = true
			}
			if _, ok := statusSet["other"]; ok && agg.Co > 0 {
				match = true
			}
			if !match {
				continue
			}
		}
		avg := agg.Sum / agg.Cnt
		r := &ApiLatencyRank{
			Method:     agg.Method,
			URI:        agg.URI,
			Count:      agg.Cnt,
			AvgLatency: avg,
			MaxLatency: agg.Max,
			Count2xx:   agg.C2,
			Count4xx:   agg.C4,
			Count5xx:   agg.C5,
			CountOther: agg.Co,
		}
		if agg.Cnt > 0 {
			r.SuccessRate = float64(agg.C2) / float64(agg.Cnt)
		}
		result = append(result, r)
	}
	return result
}

func sortLatencyRanks(items []*ApiLatencyRank, sortBy string, asc bool) {
	if len(items) == 0 {
		return
	}
	sortBy = strings.ToLower(strings.TrimSpace(sortBy))
	if sortBy == "" {
		sortBy = "avg"
	}
	less := func(i, j int) bool {
		a := items[i]
		b := items[j]
		switch sortBy {
		case "max":
			if asc {
				return a.MaxLatency < b.MaxLatency
			}
			return a.MaxLatency > b.MaxLatency
		case "count", "cnt":
			if asc {
				return a.Count < b.Count
			}
			return a.Count > b.Count
		case "2xx":
			if asc {
				return a.Count2xx < b.Count2xx
			}
			return a.Count2xx > b.Count2xx
		case "4xx":
			if asc {
				return a.Count4xx < b.Count4xx
			}
			return a.Count4xx > b.Count4xx
		case "5xx":
			if asc {
				return a.Count5xx < b.Count5xx
			}
			return a.Count5xx > b.Count5xx
		case "other":
			if asc {
				return a.CountOther < b.CountOther
			}
			return a.CountOther > b.CountOther
		case "success", "successrate":
			if asc {
				return a.SuccessRate < b.SuccessRate
			}
			return a.SuccessRate > b.SuccessRate
		default:
			if asc {
				return a.AvgLatency < b.AvgLatency
			}
			return a.AvgLatency > b.AvgLatency
		}
	}
	sort.Slice(items, less)
}

func (s *Serv) rollupWindow(currentHourStart int64) (int64, int64, bool, error) {
	if s.storageHour == nil || s.storage == nil {
		return 0, 0, false, nil
	}
	if currentHourStart <= 0 {
		return 0, 0, false, nil
	}
	endHour := currentHourStart - 3600
	if endHour <= 0 {
		return 0, 0, false, nil
	}

	minMinute, maxMinute, ok, err := s.minMaxAt(s.storage)
	if err != nil {
		return 0, 0, false, err
	}
	if !ok {
		return 0, 0, false, nil
	}
	maxMinuteHour := (maxMinute / 3600) * 3600
	if maxMinuteHour < endHour {
		endHour = maxMinuteHour
	}
	if endHour <= 0 {
		return 0, 0, false, nil
	}

	var startHour int64
	if s.lastRollup > 0 {
		startHour = s.lastRollup
	} else {
		minMinuteHour := (minMinute / 3600) * 3600
		startHour = minMinuteHour
		if maxHour, okHour, err := s.maxAt(s.storageHour); err == nil && okHour {
			if maxHour+3600 > startHour {
				startHour = maxHour + 3600
			}
		}
	}
	if startHour <= 0 || startHour > endHour {
		return 0, 0, false, nil
	}
	return startHour, endHour, true, nil
}

func (s *Serv) rollupHours(ctx context.Context, startHour, endHour int64) error {
	if startHour <= 0 || endHour < startHour {
		return nil
	}
	for h := startHour; h <= endHour; h += 3600 {
		if err := s.rollupHour(ctx, h); err != nil {
			return err
		}
	}
	return nil
}

func (s *Serv) rollupHour(ctx context.Context, hourStart int64) error {
	if s.storageHour == nil {
		return nil
	}
	hourEnd := hourStart + 3600
	count, err := s.storage.Count(map[string]any{
		"at": map[string]any{
			"$gte": hourStart,
			"$lt":  hourEnd,
		},
	})
	if err != nil {
		return err
	}
	if count == 0 {
		return nil
	}

	groupFields := latencyGroupFields()
	aggFields := latencyAggFields()
	grouped, err := s.storage.GroupByFields(map[string]any{
		"at": map[string]any{
			"$gte": hourStart,
			"$lt":  hourEnd,
		},
	}, groupFields, aggFields, 1, 0)
	if err != nil {
		return err
	}

	if err := s.storageHour.Delete(map[string]any{"at": hourStart}); err != nil {
		return err
	}

	for _, item := range grouped.Items {
		method := item.Group["method"]
		uri := item.Group["uri"]
		if method == "" || uri == "" {
			continue
		}
		stat := ApiLatencyStat{
			At:              hourStart,
			Method:          method,
			URI:             uri,
			Count:           int64(item.Value["cnt"]),
			SumLatency:      int64(item.Value["sum"]),
			MaxLatency:      int64(item.Value["max"]),
			Count2xx:        int64(item.Value["c2"]),
			SumLatency2xx:   int64(item.Value["s2"]),
			MaxLatency2xx:   int64(item.Value["m2"]),
			Count4xx:        int64(item.Value["c4"]),
			SumLatency4xx:   int64(item.Value["s4"]),
			MaxLatency4xx:   int64(item.Value["m4"]),
			Count5xx:        int64(item.Value["c5"]),
			SumLatency5xx:   int64(item.Value["s5"]),
			MaxLatency5xx:   int64(item.Value["m5"]),
			CountOther:      int64(item.Value["co"]),
			SumLatencyOther: int64(item.Value["so"]),
			MaxLatencyOther: int64(item.Value["mo"]),
		}
		if err := s.storageHour.Put(stat); err != nil {
			logger.Error(ctx, "save api latency hour stat failed", zap.Error(err), zap.String("key", method+"|"+uri))
		}
	}
	return nil
}

func (s *Serv) TopPage(ctx context.Context, start, end int64, page, size int64, methods, negMethods []string, uriPrefix, uriLike string, statuses []string, sortBy string, asc bool) (*cache.PageCache[ApiLatencyRank], *errors.Error) {

	if start == 0 || end == 0 || start > end {
		return nil, errors.Verify("invalid time range")
	}
	if size <= 0 {
		size = 10
	}
	if page <= 0 {
		page = 1
	}
	baseCond := map[string]any{}
	if len(methods) == 1 {
		baseCond["method"] = strings.ToUpper(methods[0])
	} else if len(methods) > 1 {
		var vals []string
		for _, m := range methods {
			if strings.TrimSpace(m) == "" {
				continue
			}
			vals = append(vals, strings.ToUpper(m))
		}
		if len(vals) > 0 {
			baseCond["method"] = map[string]any{"$in": vals}
		}
	}
	if len(negMethods) == 1 {
		baseCond["method"] = map[string]any{"$ne": strings.ToUpper(negMethods[0])}
	} else if len(negMethods) > 1 {
		var vals []string
		for _, m := range negMethods {
			if strings.TrimSpace(m) == "" {
				continue
			}
			vals = append(vals, strings.ToUpper(m))
		}
		if len(vals) > 0 {
			baseCond["method"] = map[string]any{"$nin": vals}
		}
	}

	if strings.TrimSpace(uriLike) != "" {
		baseCond["uri"] = map[string]any{"$like": "%" + uriLike + "%"}
	} else if strings.TrimSpace(uriPrefix) != "" {
		baseCond["uri"] = map[string]any{"$like": uriPrefix + "%"}
	}

	groupFields := latencyGroupFields()
	aggFields := latencyAggFields()

	sorter, errSort := buildLatencySorter(sortBy, asc)
	if errSort != nil {
		return nil, errSort
	}

	statusSet := buildStatusSet(statuses)
	var havingParts []string
	if len(statusSet) > 0 {
		if _, ok := statusSet["2xx"]; ok {
			havingParts = append(havingParts, "c2 > 0")
		}
		if _, ok := statusSet["4xx"]; ok {
			havingParts = append(havingParts, "c4 > 0")
		}
		if _, ok := statusSet["5xx"]; ok {
			havingParts = append(havingParts, "c5 > 0")
		}
		if _, ok := statusSet["other"]; ok {
			havingParts = append(havingParts, "co > 0")
		}
		if len(havingParts) == 0 {
			return &cache.PageCache[ApiLatencyRank]{
				Total: 0,
				Page:  page,
				Size:  size,
				Items: []*ApiLatencyRank{},
			}, nil
		}
	}

	now := time.Now().Unix()
	currentHourStart := time.Now().Truncate(time.Hour).Unix()
	useMinuteOnly := s.storageHour == nil || (start >= currentHourStart && end <= now)
	var result []*ApiLatencyRank
	var total int64

	if useMinuteOnly {
		cond := make(map[string]any, len(baseCond)+2)
		for k, v := range baseCond {
			cond[k] = v
		}
		cond["at"] = map[string]any{
			"$gte": start,
			"$lte": end,
		}
		if len(havingParts) > 0 {
			cond["$having"] = "(" + strings.Join(havingParts, " OR ") + ")"
		}

		grouped, err := s.storage.GroupByFields(cond, groupFields, aggFields, page, size, sorter)
		if err != nil {
			logger.Error(ctx, "GroupByFields latency failed", zap.Error(err))
			return nil, errors.Sys("GroupByFields latency failed", err)
		}

		result = make([]*ApiLatencyRank, 0, len(grouped.Items))
		for _, item := range grouped.Items {
			totalCount := int64(item.Value["cnt"])
			if totalCount == 0 {
				continue
			}
			totalSum := int64(item.Value["sum"])
			avg := totalSum / totalCount
			count2xx := int64(item.Value["c2"])
			count4xx := int64(item.Value["c4"])
			count5xx := int64(item.Value["c5"])
			r := &ApiLatencyRank{
				Method:     item.Group["method"],
				URI:        item.Group["uri"],
				Count:      totalCount,
				AvgLatency: avg,
				MaxLatency: int64(item.Value["max"]),
				Count2xx:   count2xx,
				Count4xx:   count4xx,
				Count5xx:   count5xx,
				CountOther: int64(item.Value["co"]),
			}
			if totalCount > 0 {
				r.SuccessRate = float64(count2xx) / float64(totalCount)
			}
			result = append(result, r)
		}
		total = grouped.Total
	} else {
		aggs := make(map[string]*latencyAgg)
		hourStart := (start / 3600) * 3600
		if end < currentHourStart {
			cond := make(map[string]any, len(baseCond)+1)
			for k, v := range baseCond {
				cond[k] = v
			}
			cond["at"] = map[string]any{
				"$gte": hourStart,
				"$lte": end,
			}
			hourRes, err := s.storageHour.GroupByFields(cond, groupFields, aggFields, 1, 0)
			if err != nil {
				logger.Error(ctx, "GroupByFields latency failed", zap.Error(err))
				return nil, errors.Sys("GroupByFields latency failed", err)
			}
			mergeLatencyItems(aggs, hourRes.Items)
		} else {
			if start < currentHourStart {
				cond := make(map[string]any, len(baseCond)+1)
				for k, v := range baseCond {
					cond[k] = v
				}
				cond["at"] = map[string]any{
					"$gte": hourStart,
					"$lt":  currentHourStart,
				}
				hourRes, err := s.storageHour.GroupByFields(cond, groupFields, aggFields, 1, 0)
				if err != nil {
					logger.Error(ctx, "GroupByFields latency failed", zap.Error(err))
					return nil, errors.Sys("GroupByFields latency failed", err)
				}
				mergeLatencyItems(aggs, hourRes.Items)
			}

			tailStart := currentHourStart
			if start > tailStart {
				tailStart = start
			}
			tailEnd := end
			if tailEnd > now {
				tailEnd = now
			}
			if tailStart <= tailEnd {
				cond := make(map[string]any, len(baseCond)+1)
				for k, v := range baseCond {
					cond[k] = v
				}
				cond["at"] = map[string]any{
					"$gte": tailStart,
					"$lte": tailEnd,
				}
				tail, err := s.storage.GroupByFields(cond, groupFields, aggFields, 1, 0)
				if err != nil {
					logger.Error(ctx, "GroupByFields latency failed", zap.Error(err))
					return nil, errors.Sys("GroupByFields latency failed", err)
				}
				mergeLatencyItems(aggs, tail.Items)
			}
		}

		result = buildLatencyRanksFromAgg(aggs, statusSet)
		sortLatencyRanks(result, sortBy, asc)
		total = int64(len(result))

		startIdx := (page - 1) * size
		if startIdx >= int64(len(result)) {
			result = []*ApiLatencyRank{}
		} else {
			endIdx := startIdx + size
			if endIdx > int64(len(result)) {
				endIdx = int64(len(result))
			}
			result = result[startIdx:endIdx]
		}
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

	return &cache.PageCache[ApiLatencyRank]{
		Total: total,
		Page:  page,
		Size:  size,
		Items: result,
	}, nil
}

func (s *Serv) Sample(ctx context.Context, method, uri string, types []string) (*ApiLatencySampleResp, *errors.Error) {
	method = strings.ToUpper(strings.TrimSpace(method))
	uri = strings.TrimSpace(uri)
	if method == "" || uri == "" {
		return nil, errors.Verify("method and uri are required")
	}

	cond := map[string]any{
		"method": method,
		"uri":    normalizeURI(uri),
	}
	meta, err := s.meta.Get(cond)
	if err != nil {
		logger.Error(ctx, "get api latency meta failed", zap.Error(err))
		return nil, errors.Sys("get api latency meta failed", err)
	}
	if meta == nil {
		return &ApiLatencySampleResp{}, nil
	}

	typeSet, errSet := buildSampleTypeSet(types)
	if errSet != nil {
		return nil, errSet
	}
	if len(typeSet) == 0 {
		typeSet = map[string]struct{}{
			"latest": {},
			"2xx":    {},
			"4xx":    {},
			"5xx":    {},
			"slow":   {},
		}
	}

	resp := &ApiLatencySampleResp{}
	if _, ok := typeSet["latest"]; ok {
		resp.Latest = meta.SampleLatest
	}
	if _, ok := typeSet["2xx"]; ok {
		resp.Sample2xx = meta.Sample2xx
	}
	if _, ok := typeSet["4xx"]; ok {
		resp.Sample4xx = meta.Sample4xx
	}
	if _, ok := typeSet["5xx"]; ok {
		resp.Sample5xx = meta.Sample5xx
	}
	if _, ok := typeSet["slow"]; ok {
		resp.SampleSlow = meta.SampleSlow
	}

	return resp, nil
}

func buildLatencySorter(sortBy string, asc bool) (cache.PageSorter, *errors.Error) {
	sortBy = strings.ToLower(strings.TrimSpace(sortBy))
	if sortBy == "" {
		sortBy = "avg"
	}
	sorter := cache.PageSorter{Asc: asc}
	switch sortBy {
	case "avg":
		sorter.Expr = "sum / NULLIF(cnt,0)"
	case "max":
		sorter.SortField = "max"
	case "count", "cnt":
		sorter.SortField = "cnt"
	case "2xx":
		sorter.SortField = "c2"
	case "4xx":
		sorter.SortField = "c4"
	case "5xx":
		sorter.SortField = "c5"
	case "other":
		sorter.SortField = "co"
	case "success", "successrate":
		sorter.Expr = "c2 / NULLIF(cnt,0)"
	default:
		return cache.PageSorter{}, errors.Verify(fmt.Sprintf("unsupported sortBy: %s", sortBy))
	}
	return sorter, nil
}

func buildLogSample(rec *logtool.LogRecord) ApiLogSample {
	if rec == nil {
		return ApiLogSample{}
	}
	data := make(map[string]string, len(rec.Data))
	for k, v := range rec.Data {
		data[k] = v
	}
	return ApiLogSample{
		Msg:   rec.Msg,
		Error: rec.Error,
		Data:  data,
	}
}

func cloneSample(sample *ApiLatencySample) *ApiLatencySample {
	if sample == nil {
		return nil
	}
	cp := *sample
	if sample.InLog.Data != nil {
		inData := make(map[string]string, len(sample.InLog.Data))
		for k, v := range sample.InLog.Data {
			inData[k] = v
		}
		cp.InLog.Data = inData
	}
	if sample.OutLog.Data != nil {
		outData := make(map[string]string, len(sample.OutLog.Data))
		for k, v := range sample.OutLog.Data {
			outData[k] = v
		}
		cp.OutLog.Data = outData
	}
	return &cp
}

func updateSampleLatest(meta *ApiLatencyMeta, sample *ApiLatencySample) {
	if meta == nil || sample == nil {
		return
	}
	if meta.SampleLatest == nil || sample.RequestAt > meta.SampleLatest.RequestAt {
		meta.SampleLatest = cloneSample(sample)
		meta.SampleTrace = sample.TraceID
		meta.SampleStatus = sample.Status
	}
}

func updateSampleByTime(dst **ApiLatencySample, sample *ApiLatencySample) {
	if sample == nil {
		return
	}
	if *dst == nil || sample.RequestAt > (*dst).RequestAt {
		*dst = cloneSample(sample)
	}
}

func updateSampleSlow(meta *ApiLatencyMeta, sample *ApiLatencySample) {
	if meta == nil || sample == nil {
		return
	}
	if meta.SampleSlow == nil ||
		sample.LatencyMs > meta.SampleSlow.LatencyMs ||
		(sample.LatencyMs == meta.SampleSlow.LatencyMs && sample.RequestAt > meta.SampleSlow.RequestAt) {
		meta.SampleSlow = cloneSample(sample)
	}
}

func mergeSampleLatest(meta *ApiLatencyMeta, sample *ApiLatencySample) {
	if meta == nil || sample == nil {
		return
	}
	if meta.SampleLatest == nil || sample.RequestAt > meta.SampleLatest.RequestAt {
		meta.SampleLatest = cloneSample(sample)
		meta.SampleTrace = sample.TraceID
		meta.SampleStatus = sample.Status
	}
}

func mergeSampleByTime(dst **ApiLatencySample, sample *ApiLatencySample) {
	if sample == nil {
		return
	}
	if *dst == nil || sample.RequestAt > (*dst).RequestAt {
		*dst = cloneSample(sample)
	}
}

func mergeSampleSlow(meta *ApiLatencyMeta, sample *ApiLatencySample) {
	if meta == nil || sample == nil {
		return
	}
	if meta.SampleSlow == nil ||
		sample.LatencyMs > meta.SampleSlow.LatencyMs ||
		(sample.LatencyMs == meta.SampleSlow.LatencyMs && sample.RequestAt > meta.SampleSlow.RequestAt) {
		meta.SampleSlow = cloneSample(sample)
	}
}

func buildSampleTypeSet(types []string) (map[string]struct{}, *errors.Error) {
	typeSet := make(map[string]struct{})
	for _, t := range types {
		val := strings.ToLower(strings.TrimSpace(t))
		if val == "" {
			continue
		}
		switch val {
		case "latest", "2xx", "4xx", "5xx", "slow":
			typeSet[val] = struct{}{}
		default:
			return nil, errors.Verify(fmt.Sprintf("unsupported type: %s", t))
		}
	}
	return typeSet, nil
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

func buildStatusSet(statuses []string) map[string]struct{} {
	statusSet := make(map[string]struct{})
	for _, st := range statuses {
		if strings.TrimSpace(st) == "" {
			continue
		}
		statusSet[strings.ToLower(st)] = struct{}{}
	}
	return statusSet
}

func hasStatusMatch(statusSet map[string]struct{}, item *cache.PageGroupItem) bool {
	if len(statusSet) == 0 {
		return true
	}
	if _, ok := statusSet["2xx"]; ok && int64(item.Value["c2"]) > 0 {
		return true
	}
	if _, ok := statusSet["4xx"]; ok && int64(item.Value["c4"]) > 0 {
		return true
	}
	if _, ok := statusSet["5xx"]; ok && int64(item.Value["c5"]) > 0 {
		return true
	}
	if _, ok := statusSet["other"]; ok && int64(item.Value["co"]) > 0 {
		return true
	}
	return false
}

func (s *Serv) countSlow(ctx context.Context, start, end time.Time, slowMs int64, excludedMethods []string) (int64, *errors.Error) {
	opts := logtool.SearchOptions{
		StartTime: start.Format(time.DateTime),
		EndTime:   end.Format(time.DateTime),
		Categories: []string{
			"rest",
			"invoke",
		},
		Levels: []string{logtool.InfoLevel.Str(), logtool.WarnLevel.Str(), logtool.ErrorLevel.Str()},
		Size:   logSearchMaxSize,
	}
	logs, e := logtool.SearchLogs(opts)
	if e != nil {
		logger.Error(ctx, "slow count search failed", zap.Error(e))
		return 0, errors.Sys("slow count search failed", e)
	}

	type inRecord struct {
		at     time.Time
		method string
	}
	type outRecord struct {
		at time.Time
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
			if tm.IsZero() {
				continue
			}
			method := strings.ToUpper(strings.TrimSpace(rec.Data["method"]))
			inMap[trace] = inRecord{at: tm, method: method}
		case "OUT":
			tm := parseTime(rec.Time)
			if tm.IsZero() {
				continue
			}
			outMap[trace] = outRecord{at: tm}
		default:
			continue
		}
	}

	var slowCount int64
	excludeSet := buildMethodSet(excludedMethods)
	for trace, in := range inMap {
		out, ok := outMap[trace]
		if !ok || in.at.IsZero() || out.at.IsZero() {
			continue
		}
		if len(excludeSet) > 0 {
			if _, ok := excludeSet[in.method]; ok {
				continue
			}
		}
		latency := out.at.Sub(in.at).Milliseconds()
		if latency < 0 {
			continue
		}
		if latency > slowMs {
			slowCount++
		}
	}
	return slowCount, nil
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

func buildMethodSet(methods []string) map[string]struct{} {
	if len(methods) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(methods))
	for _, m := range methods {
		val := strings.ToUpper(strings.TrimSpace(m))
		if val == "" {
			continue
		}
		set[val] = struct{}{}
	}
	return set
}
