package errstat

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"github.com/jom-io/gorig-om/src/logtool"
	"github.com/jom-io/gorig/cache"
	"github.com/jom-io/gorig/cronx"
	configure "github.com/jom-io/gorig/utils/cofigure"
	"github.com/jom-io/gorig/utils/errors"
	"github.com/jom-io/gorig/utils/logger"
	"go.uber.org/zap"
	"regexp"
	"strings"
	"time"
)

var serv *Serv

type Serv struct {
	storage    cache.Pager[ErrStat]
	sigStorage cache.Pager[ErrSigStat]
	sigMeta    cache.Pager[ErrSigMeta]
}

var (
	maxPeriod = 30 * 24 * time.Hour
)

func S() *Serv {
	if serv == nil {
		serv = &Serv{
			storage:    cache.NewPager[ErrStat](context.Background(), cache.Sqlite),
			sigStorage: cache.NewPager[ErrSigStat](context.Background(), cache.Sqlite, "err_sig_stat"),
			sigMeta:    cache.NewPager[ErrSigMeta](context.Background(), cache.Sqlite, "err_sig_meta"),
		}
	}
	return serv
}

func init() {
	getString := configure.GetString("om.stat.err.max_period", "720h")
	if len(getString) > 0 {
		if getMaxPeriod, err := time.ParseDuration(getString); err == nil {
			maxPeriod = getMaxPeriod
		} else {
			logger.Error(context.Background(), "Failed to parse state err MaxPeriod", zap.String("value", getString), zap.Error(err))
		}
	}

	// every minute collect host resource usage
	cronx.AddCronTask("30 * * * * *", S().Collect, 30*time.Second)

	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := S().Clear(context.Background()); err != nil {
					logger.Error(context.Background(), "ClearErrStat failed", zap.Error(err))
				}
			}
		}
	}()
}

// Collect collects error statistics
func (s *Serv) Collect(ctx context.Context) {
	// Collect error logs from the logtool within the last minute
	opts := logtool.SearchOptions{
		StartTime: time.Now().Add(-time.Minute).Format(time.DateTime),
		EndTime:   time.Now().Format(time.DateTime),
		Levels:    []string{logtool.ErrorLevel.Str(), logtool.FatalLevel.Str(), logtool.DpanicLevel.Str()},
	}
	logs, e := logtool.SearchLogs(opts)
	if e != nil {
		logger.Error(ctx, "Failed to collect error logs", zap.Error(e))
		return
	}

	errStat := &ErrStat{
		At: time.Now().Unix(),
	}
	for _, log := range logs {
		errStat.Total++
		level := logtool.Level(log.Record.Level)
		switch level {
		case logtool.ErrorLevel:
			errStat.Error++
		case logtool.FatalLevel:
			fallthrough
		case logtool.DpanicLevel:
			errStat.Panic++
		}
	}
	if err := s.storage.Put(*errStat); err != nil {
		logger.Error(ctx, "Failed to save error statistics", zap.Error(err))
		return
	}

	// signature-based aggregation
	nowBucket := time.Now().Truncate(time.Minute).Unix()
	sigAgg := make(map[string]*ErrSigStat)
	sigMeta := make(map[string]*ErrSigMeta)

	for _, log := range logs {
		level := logtool.Level(log.Record.Level).Str()
		signature := buildSignature(log.Record.Msg, log.Record.Error)
		if signature == "" {
			continue
		}
		sigHash := hashSignature(level, signature)

		if _, ok := sigAgg[sigHash]; !ok {
			sigAgg[sigHash] = &ErrSigStat{
				At:      nowBucket,
				Level:   level,
				SigHash: sigHash,
				Count:   0,
			}
			sigMeta[sigHash] = &ErrSigMeta{
				SigHash:     sigHash,
				Signature:   signature,
				Level:       level,
				SampleMsg:   log.Record.Msg,
				SampleError: log.Record.Error,
				SampleTrace: log.Record.TraceID,
				FirstAt:     nowBucket,
				LastAt:      nowBucket,
			}
		}
		sigAgg[sigHash].Count++
	}

	for hash, stat := range sigAgg {
		// upsert meta (only once per signature)
		metaCond := map[string]any{"sigHash": hash}
		existingMeta, err := s.sigMeta.Get(metaCond)
		if err != nil {
			logger.Error(ctx, "Failed to get sig meta", zap.Error(err), zap.String("sigHash", hash))
		}
		if existingMeta == nil {
			if err := s.sigMeta.Put(*sigMeta[hash]); err != nil {
				logger.Error(ctx, "Failed to save sig meta", zap.Error(err), zap.String("sigHash", hash))
			}
		} else {
			updated := *existingMeta
			updated.LastAt = nowBucket
			if updated.SampleMsg == "" && sigMeta[hash].SampleMsg != "" {
				updated.SampleMsg = sigMeta[hash].SampleMsg
			}
			if updated.SampleError == "" && sigMeta[hash].SampleError != "" {
				updated.SampleError = sigMeta[hash].SampleError
			}
			if updated.SampleTrace == "" && sigMeta[hash].SampleTrace != "" {
				updated.SampleTrace = sigMeta[hash].SampleTrace
			}
			_ = s.sigMeta.Update(metaCond, &updated)
		}

		if err := s.sigStorage.Put(*stat); err != nil {
			logger.Error(ctx, "Failed to save sig stat", zap.Error(err), zap.String("sigHash", hash))
		}
	}
}

func (s *Serv) TimeRange(ctx context.Context, start, end int64, granularity cache.Granularity, field ...ErrType) ([]*cache.PageTimeItem, *errors.Error) {
	from := time.Unix(start, 0)
	to := time.Unix(end, 0)
	logger.Info(ctx, "Host usage time range called", zap.Time("from", from), zap.Time("to", to), zap.Any("granularity", granularity), zap.Any("field", field))
	if from.IsZero() || to.IsZero() || from.After(to) {
		return nil, errors.Verify("Invalid time range")
	}
	if granularity == "" {
		granularity = cache.GranularityDay
	}
	var fieldStr []string
	for _, f := range field {
		fieldStr = append(fieldStr, string(f))
	}
	result, err := s.storage.GroupByTime(nil, from, to, granularity, cache.AggTotal, fieldStr...)
	if err != nil {
		logger.Error(ctx, "GroupByTime failed", zap.Error(err))
		return nil, errors.Sys("GroupByTime failed", err)
	}
	return result, nil
}

func (s *Serv) TopSignatures(ctx context.Context, start, end int64, filter []ErrType, limit int64) ([]*ErrSigRank, *errors.Error) {
	if start == 0 || end == 0 || start > end {
		return nil, errors.Verify("Invalid time range")
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

	if len(filter) > 0 {
		var levels []string
		for _, lv := range filter {
			if strings.TrimSpace(lv.String()) == "" {
				continue
			}
			levels = append(levels, lv.String())
		}
		if len(levels) == 1 {
			cond["level"] = levels[0]
		} else if len(levels) > 1 {
			cond["level"] = map[string]any{"$in": levels}
		}
	}

	groups := []string{"sigHash"}
	aggFields := []cache.AggField{
		{Field: "count", Agg: cache.AggSum, Alias: "cnt"},
	}

	grouped, err := s.sigStorage.GroupByFields(cond, groups, aggFields, 0, cache.PageSorterDesc("cnt"))
	if err != nil {
		logger.Error(ctx, "GroupByFields failed", zap.Error(err))
		return nil, errors.Sys("GroupByFields failed", err)
	}

	result := make([]*ErrSigRank, 0, len(grouped))
	for _, item := range grouped {
		rank := &ErrSigRank{
			SigHash: item.Group["sigHash"],
			Count:   int64(item.Value["cnt"]),
		}
		result = append(result, rank)
	}

	if int64(len(result)) > limit {
		result = result[:limit]
	}

	metaCache := make(map[string]*ErrSigMeta)
	for _, v := range result {
		if meta, ok := metaCache[v.SigHash]; ok && meta != nil {
			applyMeta(v, meta)
			continue
		}
		meta, _ := s.sigMeta.Get(map[string]any{"sigHash": v.SigHash})
		metaCache[v.SigHash] = meta
		if meta != nil {
			applyMeta(v, meta)
		}
	}

	return result, nil
}

func (s *Serv) Clear(ctx context.Context) error {
	expirationTime := time.Now().Add(-maxPeriod).Unix()
	if err := s.storage.Delete(map[string]any{"at": map[string]any{"$lt": expirationTime}}); err != nil {
		logger.Error(ctx, "Clear err stat failed", zap.Error(err))
		return err
	}

	if err := s.sigStorage.Delete(map[string]any{"at": map[string]any{"$lt": expirationTime}}); err != nil {
		logger.Error(ctx, "Clear sig stat failed", zap.Error(err))
		return err
	}

	if err := s.sigMeta.Delete(map[string]any{"lastAt": map[string]any{"$lt": expirationTime}}); err != nil {
		logger.Error(ctx, "Clear sig meta failed", zap.Error(err))
		return err
	}

	return nil
}

var (
	uuidRegexp   = regexp.MustCompile(`(?i)[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)
	numberRegexp = regexp.MustCompile(`\b\d+\b`)
	hexRegexp    = regexp.MustCompile(`0x[0-9a-fA-F]+`)
	spaceRegexp  = regexp.MustCompile(`\s+`)
)

func buildSignature(msg, errStr string) string {
	normMsg := normalizeText(msg)
	normErr := normalizeText(errStr)
	switch {
	case normMsg != "" && normErr != "":
		return normMsg + " | " + normErr
	case normMsg != "":
		return normMsg
	case normErr != "":
		return normErr
	default:
		return ""
	}
}

func normalizeText(s string) string {
	s = uuidRegexp.ReplaceAllString(s, "?")
	s = hexRegexp.ReplaceAllString(s, "?")
	s = numberRegexp.ReplaceAllString(s, "?")
	s = spaceRegexp.ReplaceAllString(strings.TrimSpace(s), " ")
	return s
}

func hashSignature(level, signature string) string {
	h := sha1.New()
	_, _ = h.Write([]byte(level + "|" + signature))
	return hex.EncodeToString(h.Sum(nil))
}

func applyMeta(rank *ErrSigRank, meta *ErrSigMeta) {
	if meta == nil {
		return
	}
	rank.Signature = meta.Signature
	if meta.Level != "" {
		rank.Level = meta.Level
	}
	rank.SampleMsg = meta.SampleMsg
	rank.SampleError = meta.SampleError
	rank.SampleTrace = meta.SampleTrace
	if meta.FirstAt != 0 {
		rank.FirstAt = meta.FirstAt
	}
	if meta.LastAt != 0 {
		rank.LastAt = meta.LastAt
	}
}
