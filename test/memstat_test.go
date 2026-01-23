package test

import (
	"context"
	"os"
	"reflect"
	"testing"
	"time"
	"unsafe"

	"github.com/jom-io/gorig-om/src/stat/memstat"
	"github.com/jom-io/gorig/cache"
)

func TestMemStatBigTop(t *testing.T) {
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

	s := newMemStatServ(t, "mem_big_stat_test", "mem_leak_event_test")
	now := time.Now().Unix()
	stats := []memstat.BigObjStat{
		{At: now - 30, Key: "a", Func: "fa", File: "a.go", Line: 10, InuseSpace: 100, InuseObjects: 1, AvgObjSize: 100},
		{At: now - 30, Key: "b", Func: "fb", File: "b.go", Line: 20, InuseSpace: 2 << 20, InuseObjects: 2, AvgObjSize: 1 << 20},
		{At: now - 30, Key: "c", Func: "fc", File: "c.go", Line: 30, InuseSpace: 3 << 20, InuseObjects: 10001, AvgObjSize: 1 << 20},
		{At: now - 10, Key: "b", Func: "fb", File: "b.go", Line: 20, InuseSpace: 3 << 20, InuseObjects: 2, AvgObjSize: 1 << 20},
	}
	for _, stat := range stats {
		if err := s.BigStorage().Put(stat); err != nil {
			t.Fatalf("put big stat failed: %v", err)
		}
	}

	result, bigErr := s.BigTop(context.Background(), now-60, now, 1, 2, "inuseSpace", false)
	if bigErr != nil {
		t.Fatalf("BigTop failed: %v", bigErr)
	}
	if result == nil || len(result.Items) != 2 {
		t.Fatalf("BigTop size mismatch: %+v", result)
	}
	if result.Items[0].Func != "fb" || result.Items[0].InuseSpace != 3<<20 {
		t.Fatalf("BigTop order mismatch: %+v", result.Items[0])
	}
	if result.Items[1].Func != "fc" || result.Items[1].InuseSpace != 3<<20 {
		t.Fatalf("BigTop second mismatch: %+v", result.Items[1])
	}
}

func TestMemStatBigCount(t *testing.T) {
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

	s := newMemStatServ(t, "mem_big_stat_test_count", "mem_leak_event_test_count")
	now := time.Now().Unix()
	stats := []memstat.BigObjStat{
		{At: now - 30, Key: "a", Func: "fa", File: "a.go", Line: 10, InuseSpace: 2 << 20, InuseObjects: 1, AvgObjSize: 1 << 20},
		{At: now - 20, Key: "a", Func: "fa", File: "a.go", Line: 10, InuseSpace: 3 << 20, InuseObjects: 2, AvgObjSize: 1 << 20},
		{At: now - 10, Key: "b", Func: "fb", File: "b.go", Line: 20, InuseSpace: 4 << 20, InuseObjects: 1, AvgObjSize: 1 << 20},
	}
	for _, stat := range stats {
		if err := s.BigStorage().Put(stat); err != nil {
			t.Fatalf("put big stat failed: %v", err)
		}
	}

	count, countErr := s.BigCount(context.Background(), now-60, now)
	if countErr != nil {
		t.Fatalf("BigCount failed: %v", countErr)
	}
	if count != 2 {
		t.Fatalf("BigCount mismatch: %d", count)
	}
}

func TestMemStatLeakLatest(t *testing.T) {
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

	s := newMemStatServ(t, "mem_big_stat_test_2", "mem_leak_event_test_2")
	now := time.Now().Unix()
	events := []memstat.LeakEvent{
		{At: now - 60, AllocBytes: 100, ObjectCount: 10, AllocDelta: 10, ObjectDelta: 1, BaseProfile: "base", LeakProfile: "leak1"},
		{At: now - 10, AllocBytes: 200, ObjectCount: 20, AllocDelta: 20, ObjectDelta: 2, BaseProfile: "base", LeakProfile: "leak2"},
	}
	for _, ev := range events {
		if err := s.LeakStorage().Put(ev); err != nil {
			t.Fatalf("put leak event failed: %v", err)
		}
	}

	latest, leakErr := s.LeakLatest(context.Background())
	if leakErr != nil {
		t.Fatalf("LeakLatest failed: %v", leakErr)
	}
	if latest == nil || latest.LeakProfile != "leak2" {
		t.Fatalf("LeakLatest mismatch: %+v", latest)
	}
}

func TestMemStatLeakCount(t *testing.T) {
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

	s := newMemStatServ(t, "mem_big_stat_test_4", "mem_leak_event_test_4")
	now := time.Now().Unix()
	events := []memstat.LeakEvent{
		{At: now - 50, LeakProfile: "leak1"},
		{At: now - 10, LeakProfile: "leak2"},
		{At: now + 10, LeakProfile: "leak3"},
	}
	for _, ev := range events {
		if err := s.LeakStorage().Put(ev); err != nil {
			t.Fatalf("put leak event failed: %v", err)
		}
	}

	count, countErr := s.LeakCount(context.Background(), now-60, now)
	if countErr != nil {
		t.Fatalf("LeakCount failed: %v", countErr)
	}
	if count != 2 {
		t.Fatalf("LeakCount mismatch: %d", count)
	}
}

func TestMemStatLeakPage(t *testing.T) {
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

	s := newMemStatServ(t, "mem_big_stat_test_3", "mem_leak_event_test_3")
	now := time.Now().Unix()
	events := []memstat.LeakEvent{
		{At: now - 20, LeakProfile: "leak1"},
		{At: now - 10, LeakProfile: "leak2"},
		{At: now - 5, LeakProfile: "leak3"},
	}
	for _, ev := range events {
		if err := s.LeakStorage().Put(ev); err != nil {
			t.Fatalf("put leak event failed: %v", err)
		}
	}

	page, leakErr := s.LeakPage(context.Background(), now-30, now, 1, 2)
	if leakErr != nil {
		t.Fatalf("LeakPage failed: %v", leakErr)
	}
	if page == nil || len(page.Items) != 2 {
		t.Fatalf("LeakPage size mismatch: %+v", page)
	}
	if page.Items[0].LeakProfile != "leak3" {
		t.Fatalf("LeakPage order mismatch: %+v", page.Items[0])
	}
	if page.Items[1].LeakProfile != "leak2" {
		t.Fatalf("LeakPage second mismatch: %+v", page.Items[1])
	}
}

type memStatServWrapper struct {
	*memstat.Serv
}

func newMemStatServ(t *testing.T, bigName, leakName string) memStatServWrapper {
	t.Helper()
	s := &memstat.Serv{}
	bigStorage := cache.NewPager[memstat.BigObjStat](context.Background(), cache.Sqlite, bigName)
	leakStorage := cache.NewPager[memstat.LeakEvent](context.Background(), cache.Sqlite, leakName)

	val := reflect.ValueOf(s).Elem()
	setUnexportedMemField(val.FieldByName("bigStorage"), bigStorage)
	setUnexportedMemField(val.FieldByName("leakStorage"), leakStorage)
	return memStatServWrapper{Serv: s}
}

func setUnexportedMemField(field reflect.Value, value interface{}) {
	ptr := unsafe.Pointer(field.UnsafeAddr())
	reflect.NewAt(field.Type(), ptr).Elem().Set(reflect.ValueOf(value))
}

func (s memStatServWrapper) BigStorage() cache.Pager[memstat.BigObjStat] {
	val := reflect.ValueOf(s.Serv).Elem()
	field := val.FieldByName("bigStorage")
	ptr := unsafe.Pointer(field.UnsafeAddr())
	storage := reflect.NewAt(field.Type(), ptr).Elem().Interface()
	return storage.(cache.Pager[memstat.BigObjStat])
}

func (s memStatServWrapper) LeakStorage() cache.Pager[memstat.LeakEvent] {
	val := reflect.ValueOf(s.Serv).Elem()
	field := val.FieldByName("leakStorage")
	ptr := unsafe.Pointer(field.UnsafeAddr())
	storage := reflect.NewAt(field.Type(), ptr).Elem().Interface()
	return storage.(cache.Pager[memstat.LeakEvent])
}
