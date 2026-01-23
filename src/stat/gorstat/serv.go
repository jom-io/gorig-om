package gorstat

import (
	"context"
	"math"
	"runtime"
	"time"

	"github.com/jom-io/gorig/cache"
	"github.com/jom-io/gorig/cronx"
	configure "github.com/jom-io/gorig/utils/cofigure"
	"github.com/jom-io/gorig/utils/errors"
	"github.com/jom-io/gorig/utils/logger"
	"go.uber.org/zap"
)

var (
	serv      *Serv
	maxPeriod = 30 * 24 * time.Hour
)

type Serv struct {
	storage cache.Pager[GoroutineStat]
}

func S() *Serv {
	if serv == nil {
		serv = &Serv{
			storage: cache.NewPager[GoroutineStat](context.Background(), cache.Sqlite, "goroutine_stat"),
		}
	}
	return serv
}

func init() {
	getString := configure.GetString("om.stat.goroutine.max_period", "720h")
	if len(getString) > 0 {
		if getMaxPeriod, err := time.ParseDuration(getString); err == nil {
			maxPeriod = getMaxPeriod
		} else {
			logger.Error(context.Background(), "Failed to parse goroutine MaxPeriod", zap.String("value", getString), zap.Error(err))
		}
	}

	cronx.AddCronTask("*/30 * * * * *", S().Collect, 10*time.Second)

	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			if err := S().Clear(context.Background()); err != nil {
				logger.Error(context.Background(), "Clear goroutine stat failed", zap.Error(err))
			}
		}
	}()
}

func SetMaxPeriod(d time.Duration) {
	if d <= 0 {
		logger.Error(context.Background(), "SetMaxPeriod called with non-positive duration", zap.Duration("duration", d))
		return
	}
	maxPeriod = d
	logger.Info(context.Background(), "Goroutine MaxPeriod set", zap.Duration("maxPeriod", maxPeriod))
}

func (s *Serv) Collect(ctx context.Context) {
	stat := GoroutineStat{
		At:    time.Now().Unix(),
		Count: int64(runtime.NumGoroutine()),
	}
	if err := s.storage.Put(stat); err != nil {
		logger.Error(ctx, "Save goroutine stat failed", zap.Error(err))
	}
}

func (s *Serv) TimeRange(ctx context.Context, start, end int64, granularity cache.Granularity) ([]*cache.PageTimeItem, *errors.Error) {
	from := time.Unix(start, 0)
	to := time.Unix(end, 0)
	if from.IsZero() || to.IsZero() || from.After(to) {
		return nil, errors.Verify("Invalid time range")
	}
	if granularity == "" {
		granularity = cache.GranularityMinute
	}
	result, err := s.storage.GroupByTime(nil, from, to, granularity, cache.AggAvg, "count")
	if err != nil {
		logger.Error(ctx, "GroupByTime goroutine stat failed", zap.Error(err))
		return nil, errors.Sys("GroupByTime failed", err)
	}
	for _, item := range result {
		if item == nil || item.Value == nil {
			continue
		}
		if val, ok := item.Value["count"]; ok {
			item.Value["count"] = math.Round(val)
		}
	}
	return result, nil
}

func (s *Serv) Clear(ctx context.Context) error {
	expirationTime := time.Now().Add(-maxPeriod).Unix()
	if err := s.storage.Delete(map[string]any{"at": map[string]any{"$lt": expirationTime}}); err != nil {
		logger.Error(ctx, "Clear goroutine stat failed", zap.Error(err))
		return err
	}
	return nil
}

func (s *Serv) Count(ctx context.Context) (int64, *errors.Error) {
	count, err := s.storage.Count(nil)
	if err != nil {
		logger.Error(ctx, "Count goroutine stat failed", zap.Error(err))
		return 0, errors.Sys("Count failed", err)
	}
	return count, nil
}
