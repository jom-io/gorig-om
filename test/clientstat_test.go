package test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"
	"time"
	"unsafe"

	"github.com/jom-io/gorig-om/src/stat/clientstat"
	"github.com/jom-io/gorig/cache"
)

func TestClientStatIPTrend(t *testing.T) {
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

	s := newClientStatServ(t, "client_day_stat_test", "client_stat_meta_test")
	day1 := time.Date(2026, 1, 10, 0, 0, 0, 0, time.Local).Unix()
	day2 := time.Date(2026, 1, 11, 0, 0, 0, 0, time.Local).Unix()

	stats := []clientstat.ClientDayStat{
		{At: day1, IPCount: 10},
		{At: day2, IPCount: 20},
	}
	for _, stat := range stats {
		if err := s.Storage().Put(stat); err != nil {
			t.Fatalf("put client day stat failed: %v", err)
		}
	}

	items, statErr := s.IPTrend(context.Background(), day1-10, day2+10)
	if statErr != nil {
		t.Fatalf("IPTrend failed: %v", statErr)
	}
	if len(items) != 2 {
		t.Fatalf("IPTrend size mismatch: %+v", items)
	}
	if items[0].At != time.Unix(day1, 0).Format("2006-01-02") {
		t.Fatalf("IPTrend day1 mismatch: %+v", items[0])
	}
	if items[1].At != time.Unix(day2, 0).Format("2006-01-02") {
		t.Fatalf("IPTrend day2 mismatch: %+v", items[1])
	}
	if items[0].Value["count"] != 10 || items[1].Value["count"] != 20 {
		t.Fatalf("IPTrend count mismatch: %+v", items)
	}
}

func TestClientStatIPTopLimit(t *testing.T) {
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

	s := newClientStatServ(t, "client_day_stat_test_top", "client_stat_meta_test_top")
	day := time.Date(2026, 1, 12, 0, 0, 0, 0, time.Local).Unix()
	top := make([]clientstat.ClientIPTop, 0, 120)
	for i := 1; i <= 120; i++ {
		top = append(top, clientstat.ClientIPTop{
			IP:    fmt.Sprintf("ip-%03d", i),
			Count: int64(i),
		})
	}
	stat := clientstat.ClientDayStat{At: day, Top: top}
	if err := s.Storage().Put(stat); err != nil {
		t.Fatalf("put client day stat failed: %v", err)
	}

	dayStr := time.Unix(day, 0).Format("2006-01-02")
	items, statErr := s.IPTop(context.Background(), dayStr, 200)
	if statErr != nil {
		t.Fatalf("IPTop failed: %v", statErr)
	}
	if len(items) != 100 {
		t.Fatalf("IPTop limit mismatch: %d", len(items))
	}
	if items[0].Count != 120 || items[len(items)-1].Count != 21 {
		t.Fatalf("IPTop order mismatch: %+v", items)
	}

	items, statErr = s.IPTop(context.Background(), dayStr, 10)
	if statErr != nil {
		t.Fatalf("IPTop failed: %v", statErr)
	}
	if len(items) != 10 {
		t.Fatalf("IPTop limit 10 mismatch: %d", len(items))
	}
	if items[0].Count != 120 || items[len(items)-1].Count != 111 {
		t.Fatalf("IPTop limit 10 order mismatch: %+v", items)
	}
}

func TestClientStatDist(t *testing.T) {
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

	s := newClientStatServ(t, "client_day_stat_test_dist", "client_stat_meta_test_dist")
	day := time.Date(2026, 1, 13, 0, 0, 0, 0, time.Local).Unix()
	stat := clientstat.ClientDayStat{
		At: day,
		DeviceDist: map[string]int64{
			"Android": 5,
			"iPhone":  9,
			"Other":   2,
		},
		RegionDist: map[string]int64{
			"Zhejiang/Hangzhou": 7,
			"Shanghai":          3,
		},
	}
	if err := s.Storage().Put(stat); err != nil {
		t.Fatalf("put client day stat failed: %v", err)
	}

	dayStr := time.Unix(day, 0).Format("2006-01-02")
	devices, statErr := s.DeviceDist(context.Background(), dayStr)
	if statErr != nil {
		t.Fatalf("DeviceDist failed: %v", statErr)
	}
	if len(devices) != 3 {
		t.Fatalf("DeviceDist size mismatch: %+v", devices)
	}
	if devices[0].Name != "iPhone" || devices[1].Name != "Android" || devices[2].Name != "Other" {
		t.Fatalf("DeviceDist order mismatch: %+v", devices)
	}

	regions, statErr := s.RegionDist(context.Background(), dayStr)
	if statErr != nil {
		t.Fatalf("RegionDist failed: %v", statErr)
	}
	if len(regions) != 2 {
		t.Fatalf("RegionDist size mismatch: %+v", regions)
	}
	if regions[0].Name != "Zhejiang/Hangzhou" || regions[1].Name != "Shanghai" {
		t.Fatalf("RegionDist order mismatch: %+v", regions)
	}
}

func TestClientStatIPDBInit(t *testing.T) {
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

	s := newClientStatServ(t, "client_day_stat_test_ipdb", "client_stat_meta_test_ipdb")
	v4Payload := []byte("ipdb-v4-test")
	v6Payload := []byte("ipdb-v6-test")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v4.xdb":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(v4Payload)
		case "/v6.xdb":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(v6Payload)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	if err := s.InitIPDB(context.Background(), server.URL+"/v4.xdb", server.URL+"/v6.xdb"); err != nil {
		t.Fatalf("InitIPDB failed: %v", err)
	}
	v4Path := ".cache/ip/ip2region_v4.xdb"
	data, err := os.ReadFile(v4Path)
	if err != nil {
		t.Fatalf("read ipdb v4 failed: %v", err)
	}
	if string(data) != string(v4Payload) {
		t.Fatalf("ipdb v4 content mismatch: %s", string(data))
	}
	v6Path := ".cache/ip/ip2region_v6.xdb"
	data, err = os.ReadFile(v6Path)
	if err != nil {
		t.Fatalf("read ipdb v6 failed: %v", err)
	}
	if string(data) != string(v6Payload) {
		t.Fatalf("ipdb v6 content mismatch: %s", string(data))
	}
}

type clientStatServWrapper struct {
	*clientstat.Serv
}

func newClientStatServ(t *testing.T, dayName, metaName string) clientStatServWrapper {
	t.Helper()
	s := &clientstat.Serv{}
	storage := cache.NewPager[clientstat.ClientDayStat](context.Background(), cache.Sqlite, dayName)
	meta := cache.New[clientstat.ClientStatCursor](cache.Sqlite, metaName)

	val := reflect.ValueOf(s).Elem()
	setUnexportedClientField(val.FieldByName("storage"), storage)
	setUnexportedClientField(val.FieldByName("meta"), meta)
	return clientStatServWrapper{Serv: s}
}

func setUnexportedClientField(field reflect.Value, value interface{}) {
	ptr := unsafe.Pointer(field.UnsafeAddr())
	reflect.NewAt(field.Type(), ptr).Elem().Set(reflect.ValueOf(value))
}

func (s clientStatServWrapper) Storage() cache.Pager[clientstat.ClientDayStat] {
	val := reflect.ValueOf(s.Serv).Elem()
	field := val.FieldByName("storage")
	ptr := unsafe.Pointer(field.UnsafeAddr())
	storage := reflect.NewAt(field.Type(), ptr).Elem().Interface()
	return storage.(cache.Pager[clientstat.ClientDayStat])
}
