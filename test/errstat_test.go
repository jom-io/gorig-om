package test

import (
	"context"
	"github.com/jom-io/gorig-om/src/stat/errstat"
	"github.com/jom-io/gorig/utils/logger"
	"testing"
	"time"

	"github.com/jom-io/gorig/cache"
)

func TestErrStatWorkflow(t *testing.T) {
	ctx := context.Background()
	s := errstat.S()

	t.Run("CollectErrorStats", func(t *testing.T) {
		logger.Warn(ctx, "Starting error stats collection")
		logger.Error(ctx, "Simulating error for testing")
		logger.DPanic(ctx, "Simulating panic for testing")

		s.Collect(ctx)
		t.Log("Collect executed")
	})

	t.Run("ValidTimeRangeQuery", func(t *testing.T) {
		end := time.Now().Unix()
		start := end - 60
		result, err := s.TimeRange(ctx, start, end, cache.GranularityMinute, errstat.ErrTypeWarn, errstat.ErrTypeError, errstat.ErrTypePanic, errstat.ErrTypeTotal)
		if err != nil {
			t.Errorf("TimeRange failed: %v", err)
			return
		}
		t.Logf("TimeRange returned %d items", len(result))
		for _, item := range result {
			t.Logf("TimeRange item: %v", item)
		}
	})

	t.Run("InvalidTimeRangeQuery", func(t *testing.T) {
		start := time.Now().Unix()
		end := start - 3600
		_, err := s.TimeRange(ctx, start, end, "")
		if err == nil {
			t.Errorf("Expected verify error, got: %v", err)
		} else {
			t.Logf("Invalid range correctly detected: %v", err)
		}
	})

	t.Run("ClearOldStats", func(t *testing.T) {
		if err := s.Clear(ctx); err != nil {
			t.Errorf("Clear failed: %v", err)
		} else {
			t.Log("Clear executed successfully")
		}
	})
}
