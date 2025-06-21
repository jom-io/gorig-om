package errstat

import (
	"context"
	"github.com/jom-io/gorig-om/src/logtool"
	"github.com/jom-io/gorig/cache"
	"github.com/jom-io/gorig/cronx"
	configure "github.com/jom-io/gorig/utils/cofigure"
	"github.com/jom-io/gorig/utils/errors"
	"github.com/jom-io/gorig/utils/logger"
	"go.uber.org/zap"
	"time"
)

var serv *Serv

type Serv struct {
	storage cache.Pager[ErrStat]
}

var (
	maxPeriod = 30 * 24 * time.Hour
)

func S() *Serv {
	if serv == nil {
		return &Serv{
			storage: cache.NewPager[ErrStat](context.Background(), cache.Sqlite),
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
	cronx.AddCronTask("0 * * * * *", S().Collect, 10*time.Second)

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
		Levels:    []string{logtool.WarnLevel.Str(), logtool.ErrorLevel.Str(), logtool.FatalLevel.Str(), logtool.DebugLevel.Str()},
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
		case logtool.WarnLevel:
			errStat.Warn++
		case logtool.ErrorLevel:
			errStat.Error++
		case logtool.FatalLevel:
			fallthrough
		case logtool.DebugLevel:
			errStat.Panic++
		}
	}
	if err := s.storage.Put(*errStat); err != nil {
		logger.Error(ctx, "Failed to save error statistics", zap.Error(err))
		return
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
	result, err := s.storage.GroupByTime(nil, from, to, granularity, fieldStr...)
	if err != nil {
		logger.Error(ctx, "GroupByTime failed", zap.Error(err))
		return nil, errors.Sys("GroupByTime failed", err)
	}
	return result, nil
}

func (s *Serv) Clear(ctx context.Context) error {
	expirationTime := time.Now().Add(-maxPeriod).Unix()
	if err := s.storage.Delete(map[string]any{"time": map[string]any{"$lt": expirationTime}}); err != nil {
		logger.Error(ctx, "Clear err stat failed", zap.Error(err))
		return err
	}

	return nil
}
