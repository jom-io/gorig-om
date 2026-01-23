package test

import (
	"context"
	"os"
	"reflect"
	"testing"
	"time"
	"unsafe"

	"github.com/jom-io/gorig-om/src/stat/gorstat"
	"github.com/jom-io/gorig/cache"
)

func TestGorStatTimeRange(t *testing.T) {
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

	s := newGorStatServ(t, "goroutine_stat_test")
	now := time.Now().Unix()
	stats := []gorstat.GoroutineStat{
		{At: now, Count: 10},
		{At: now, Count: 30},
	}
	for _, stat := range stats {
		if err := s.Storage().Put(stat); err != nil {
			t.Fatalf("put goroutine stat failed: %v", err)
		}
	}

	start := time.Now().Add(-time.Hour).Unix()
	end := time.Now().Add(time.Hour).Unix()
	items, timeErr := s.TimeRange(context.Background(), start, end, cache.GranularityHour)
	if timeErr != nil {
		t.Fatalf("TimeRange failed: %v", timeErr)
	}
	if len(items) != 1 {
		t.Fatalf("unexpected time range size: %d", len(items))
	}
	val, ok := items[0].Value["count"]
	if !ok {
		t.Fatalf("count value missing: %+v", items[0].Value)
	}
	if val < 19.5 || val > 20.5 {
		t.Fatalf("unexpected avg count: %v", val)
	}
}

func TestGorStatClear(t *testing.T) {
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

	gorstat.SetMaxPeriod(2 * time.Second)
	t.Cleanup(func() {
		gorstat.SetMaxPeriod(30 * 24 * time.Hour)
	})

	s := newGorStatServ(t, "goroutine_stat_test_clear")
	now := time.Now().Unix()
	stats := []gorstat.GoroutineStat{
		{At: now - 3600, Count: 10},
		{At: now, Count: 20},
	}
	for _, stat := range stats {
		if err := s.Storage().Put(stat); err != nil {
			t.Fatalf("put goroutine stat failed: %v", err)
		}
	}

	if err := s.Clear(context.Background()); err != nil {
		t.Fatalf("Clear failed: %v", err)
	}
	count, err := s.Storage().Count(nil)
	if err != nil {
		t.Fatalf("Count failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("unexpected count after clear: %d", count)
	}
}

type gorStatServWrapper struct {
	*gorstat.Serv
}

func newGorStatServ(t *testing.T, name string) gorStatServWrapper {
	t.Helper()
	s := &gorstat.Serv{}
	storage := cache.NewPager[gorstat.GoroutineStat](context.Background(), cache.Sqlite, name)

	val := reflect.ValueOf(s).Elem()
	setUnexportedGorField(val.FieldByName("storage"), storage)
	return gorStatServWrapper{Serv: s}
}

func setUnexportedGorField(field reflect.Value, value interface{}) {
	ptr := unsafe.Pointer(field.UnsafeAddr())
	reflect.NewAt(field.Type(), ptr).Elem().Set(reflect.ValueOf(value))
}

func (s gorStatServWrapper) Storage() cache.Pager[gorstat.GoroutineStat] {
	val := reflect.ValueOf(s.Serv).Elem()
	field := val.FieldByName("storage")
	ptr := unsafe.Pointer(field.UnsafeAddr())
	storage := reflect.NewAt(field.Type(), ptr).Elem().Interface()
	return storage.(cache.Pager[gorstat.GoroutineStat])
}
