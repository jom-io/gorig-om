package clientstat

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"math"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jom-io/gorig-om/src/logtool"
	"github.com/jom-io/gorig/cache"
	"github.com/jom-io/gorig/utils/errors"
	"github.com/jom-io/gorig/utils/logger"
	"github.com/lionsoul2014/ip2region/binding/golang/service"
	"go.uber.org/zap"
)

const (
	clientCollectSpec   = "22 */10 * * * *"
	clientCollectPeriod = 10 * time.Minute
	clientCollectDelay  = 2 * time.Minute
	clientLogMaxSize    = 50000
	clientTopLimit      = 100
	clientMaxPeriod     = 90 * 24 * time.Hour
	clientIpdbTimeout   = 30 * time.Second

	ipSketchBits  = 1 << 20
	ipSketchBytes = ipSketchBits / 8

	ipdbDirName   = "ip"
	ipdbV4Name    = "ip2region_v4.xdb"
	ipdbV6Name    = "ip2region_v6.xdb"
	cursorKey     = "clientstat_cursor"
	unknownDevice = "unknown"
	unknownRegion = "unknown"

	defaultV4URL = "https://github.com/lionsoul2014/ip2region/raw/master/data/ip2region_v4.xdb"
	defaultV6URL = "https://github.com/lionsoul2014/ip2region/raw/master/data/ip2region_v6.xdb"
)

var (
	serv           *Serv
	androidModelRe = regexp.MustCompile(`Android [^;]+;\s*([^;]+?)\s+Build/`)
	iosVersionRe   = regexp.MustCompile(`iPhone OS ([0-9_]+)`)
)

type Serv struct {
	storage cache.Pager[ClientDayStat]
	meta    cache.Cache[ClientStatCursor]
	mu      sync.RWMutex
	ipdb    *service.Ip2Region
}

func S() *Serv {
	if serv == nil {
		serv = &Serv{
			storage: cache.NewPager[ClientDayStat](context.Background(), cache.Sqlite, "client_day_stat"),
			meta:    cache.New[ClientStatCursor](cache.Sqlite, "client_stat_meta"),
		}
	}
	return serv
}

func init() {
	//cronx.AddCronTask(clientCollectSpec, S().Collect, clientCollectDelay)
	//go func() {
	//	ticker := time.NewTicker(time.Minute)
	//	defer ticker.Stop()
	//	for range ticker.C {
	//		if err := S().Clear(context.Background()); err != nil {
	//			logger.Error(context.Background(), "Clear client stat failed", zap.Error(err))
	//		}
	//	}
	//}()
}

type dayAgg struct {
	stat   ClientDayStat
	sketch *ipSketch
	top    *topCounter
	exists bool
}

type batchIP struct {
	count  int64
	device string
	region string
}

func (s *Serv) Collect(ctx context.Context) {
	now := time.Now()
	start := now.Add(-clientCollectPeriod)

	cursor, err := s.meta.Get(cursorKey)
	if err == nil && cursor.LastAt > 0 {
		start = time.Unix(cursor.LastAt, 0)
	}
	if cursor.LastPath != "" {
		if _, err := os.Stat(cursor.LastPath); err != nil {
			cursor.LastPath = ""
			cursor.LastLine = 0
		}
	}

	opts := logtool.SearchOptions{
		StartTime: start.Format(time.DateTime),
		EndTime:   now.Format(time.DateTime),
		Categories: []string{
			"rest",
		},
		Levels: []string{logtool.InfoLevel.Str(), logtool.WarnLevel.Str(), logtool.ErrorLevel.Str()},
		Size:   clientLogMaxSize,
	}
	if cursor.LastPath != "" && cursor.LastLine > 0 {
		opts.LastPath = cursor.LastPath
		opts.LastLine = cursor.LastLine
	}

	aggs := make(map[int64]*dayAgg)
	for {
		logs, e := logtool.SearchLogs(opts)
		if e != nil {
			logger.Error(ctx, "client stat collect search failed", zap.Error(e))
			return
		}
		if len(logs) == 0 {
			break
		}

		batches := make(map[int64]map[string]*batchIP)
		for _, item := range logs {
			rec := item.Record
			if rec == nil {
				continue
			}
			if strings.ToUpper(strings.TrimSpace(rec.Msg)) != "IN" {
				continue
			}
			tm := parseTime(rec.Time)
			if tm.IsZero() {
				continue
			}
			dayAt := dayStartAt(tm)
			headers := parseHeaders(rec.Data["header"])
			ip := resolveIP(headers, rec.Data["remoteAddr"])
			if ip == "" {
				continue
			}
			ua := firstHeader(headers, "user-agent")
			device := parseDevice(ua)
			region := s.lookupRegion(ip)

			dayBatch := batches[dayAt]
			if dayBatch == nil {
				dayBatch = make(map[string]*batchIP)
				batches[dayAt] = dayBatch
			}
			entry := dayBatch[ip]
			if entry == nil {
				entry = &batchIP{device: device, region: region}
				dayBatch[ip] = entry
			}
			entry.count++
			entry.device = device
			entry.region = region
		}

		for dayAt, dayBatch := range batches {
			agg, err := s.getDayAgg(ctx, dayAt, aggs)
			if err != nil {
				continue
			}
			for ip, entry := range dayBatch {
				agg.sketch.Add(ip)
				device := entry.device
				if device == "" {
					device = unknownDevice
				}
				region := entry.region
				if region == "" {
					region = unknownRegion
				}
				agg.top.Add(ip, entry.count, device, region)
				agg.stat.DeviceDist[device] += entry.count
				agg.stat.RegionDist[region] += entry.count
			}
			aggs[dayAt] = agg
		}

		last := logs[len(logs)-1]
		if last.Record != nil {
			if lastAt := parseTime(last.Record.Time); !lastAt.IsZero() {
				cursor.LastAt = lastAt.Unix()
			}
		}
		cursor.LastPath = last.FilePath
		cursor.LastLine = last.LineNumber
		if len(logs) < clientLogMaxSize {
			break
		}
		opts.LastPath = cursor.LastPath
		opts.LastLine = cursor.LastLine
	}

	if cursor.LastPath != "" {
		if err := s.meta.Set(cursorKey, cursor, 0); err != nil {
			logger.Error(ctx, "save client stat cursor failed", zap.Error(err))
		}
	}

	for dayAt, agg := range aggs {
		stat := agg.stat
		stat.IPSketch = agg.sketch.Encode()
		stat.IPCount = agg.sketch.Estimate()
		stat.Top = agg.top.List()
		stat.UpdatedAt = time.Now().Unix()
		cond := map[string]any{"at": dayAt}
		if agg.exists {
			if err := s.storage.Update(cond, &stat); err != nil {
				logger.Error(ctx, "update client day stat failed", zap.Error(err))
			}
		} else {
			if err := s.storage.Put(stat); err != nil {
				logger.Error(ctx, "save client day stat failed", zap.Error(err))
			}
		}
	}
}

func (s *Serv) IPTrend(ctx context.Context, start, end int64) ([]*cache.PageTimeItem, *errors.Error) {
	from := time.Unix(start, 0)
	to := time.Unix(end, 0)
	if from.IsZero() || to.IsZero() || from.After(to) {
		return nil, errors.Verify("invalid time range")
	}

	cond := map[string]any{
		"at": map[string]any{
			"$gte": start,
			"$lte": end,
		},
	}
	page, err := s.storage.Find(1, 1000, cond, cache.PageSorterAsc("at"))
	if err != nil {
		logger.Error(ctx, "Find client ip trend failed", zap.Error(err))
		return nil, errors.Sys("Find ip trend failed", err)
	}
	if page == nil || len(page.Items) == 0 {
		return []*cache.PageTimeItem{}, nil
	}
	result := make([]*cache.PageTimeItem, 0, len(page.Items))
	for _, item := range page.Items {
		if item == nil {
			continue
		}
		result = append(result, &cache.PageTimeItem{
			At: time.Unix(item.At, 0).In(time.Local).Format("2006-01-02"),
			Value: map[string]float64{
				"count": float64(item.IPCount),
			},
		})
	}
	return result, nil
}

func (s *Serv) IPTop(ctx context.Context, day string, limit int64) ([]ClientIPTop, *errors.Error) {
	dayAt, err := parseDay(day)
	if err != nil {
		return nil, errors.Verify("invalid date")
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > clientTopLimit {
		limit = clientTopLimit
	}
	stat, statErr := s.getDayStat(ctx, dayAt)
	if statErr != nil {
		return nil, statErr
	}
	if stat == nil || len(stat.Top) == 0 {
		return []ClientIPTop{}, nil
	}
	top := append([]ClientIPTop(nil), stat.Top...)
	sort.Slice(top, func(i, j int) bool {
		return top[i].Count > top[j].Count
	})
	if int64(len(top)) > limit {
		top = top[:limit]
	}
	return top, nil
}

func (s *Serv) DeviceDist(ctx context.Context, day string) ([]ClientDistItem, *errors.Error) {
	return s.distByField(ctx, day, func(stat *ClientDayStat) map[string]int64 {
		return stat.DeviceDist
	})
}

func (s *Serv) RegionDist(ctx context.Context, day string) ([]ClientDistItem, *errors.Error) {
	return s.distByField(ctx, day, func(stat *ClientDayStat) map[string]int64 {
		return stat.RegionDist
	})
}

func (s *Serv) distByField(ctx context.Context, day string, getter func(*ClientDayStat) map[string]int64) ([]ClientDistItem, *errors.Error) {
	dayAt, err := parseDay(day)
	if err != nil {
		return nil, errors.Verify("invalid date")
	}
	stat, statErr := s.getDayStat(ctx, dayAt)
	if statErr != nil {
		return nil, statErr
	}
	if stat == nil {
		return []ClientDistItem{}, nil
	}
	dist := getter(stat)
	if len(dist) == 0 {
		return []ClientDistItem{}, nil
	}
	items := make([]ClientDistItem, 0, len(dist))
	for k, v := range dist {
		items = append(items, ClientDistItem{Name: k, Count: v})
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].Count > items[j].Count
	})
	return items, nil
}

func (s *Serv) Clear(ctx context.Context) error {
	expirationTime := time.Now().Add(-clientMaxPeriod).Unix()
	if err := s.storage.Delete(map[string]any{"at": map[string]any{"$lt": expirationTime}}); err != nil {
		logger.Error(ctx, "Clear client stat failed", zap.Error(err))
		return err
	}
	return nil
}

func (s *Serv) InitIPDB(ctx context.Context, v4URL, v6URL string) *errors.Error {
	v4URL = strings.TrimSpace(v4URL)
	v6URL = strings.TrimSpace(v6URL)
	if v4URL == "" {
		v4URL = defaultV4URL
	}
	if v6URL == "" {
		v6URL = defaultV6URL
	}
	dir, err := ensureIPDBDir()
	if err != nil {
		return errors.Sys("ensure ipdb dir failed", err)
	}
	v4Path := filepath.Join(dir, ipdbV4Name)
	if err := downloadFile(ctx, v4URL, v4Path); err != nil {
		return errors.Sys("download ipdb v4 failed", err)
	}
	v6Path := filepath.Join(dir, ipdbV6Name)
	if err := downloadFile(ctx, v6URL, v6Path); err != nil {
		logger.Warn(ctx, "download ipdb v6 failed", zap.Error(err))
	}
	s.mu.Lock()
	if s.ipdb != nil {
		s.ipdb.Close()
		s.ipdb = nil
	}
	s.mu.Unlock()
	return nil
}

func (s *Serv) getDayAgg(ctx context.Context, dayAt int64, aggs map[int64]*dayAgg) (*dayAgg, error) {
	if agg, ok := aggs[dayAt]; ok {
		return agg, nil
	}
	stat, err := s.getDayStat(ctx, dayAt)
	if err != nil {
		return nil, err
	}
	agg := &dayAgg{
		stat: ClientDayStat{
			At:         dayAt,
			DeviceDist: map[string]int64{},
			RegionDist: map[string]int64{},
		},
		sketch: newIPSketch(),
		top:    newTopCounter(clientTopLimit),
	}
	if stat != nil {
		agg.exists = true
		agg.stat = *stat
		if agg.stat.DeviceDist == nil {
			agg.stat.DeviceDist = map[string]int64{}
		}
		if agg.stat.RegionDist == nil {
			agg.stat.RegionDist = map[string]int64{}
		}
		agg.sketch = decodeIPSketch(stat.IPSketch)
		agg.top = newTopCounter(clientTopLimit)
		agg.top.Load(stat.Top)
	}
	return agg, nil
}

func (s *Serv) getDayStat(ctx context.Context, dayAt int64) (*ClientDayStat, *errors.Error) {
	cond := map[string]any{"at": dayAt}
	stat, err := s.storage.Get(cond)
	if err != nil {
		logger.Error(ctx, "Get client day stat failed", zap.Error(err))
		return nil, errors.Sys("Get client day stat failed", err)
	}
	return stat, nil
}

func (s *Serv) lookupRegion(ip string) string {
	if ip == "" {
		return unknownRegion
	}
	ipdb, err := s.getIPDB()
	if err != nil || ipdb == nil {
		return unknownRegion
	}
	region, err := ipdb.SearchByStr(ip)
	if err != nil {
		return unknownRegion
	}
	region = normalizeRegion(region)
	if region == "" {
		return unknownRegion
	}
	return region
}

func (s *Serv) getIPDB() (*service.Ip2Region, error) {
	s.mu.RLock()
	if s.ipdb != nil {
		ipdb := s.ipdb
		s.mu.RUnlock()
		return ipdb, nil
	}
	s.mu.RUnlock()

	v4Path := ipdbPath(ipdbV4Name)
	v6Path := ipdbPath(ipdbV6Name)
	if v4Path == "" && v6Path == "" {
		return nil, fmt.Errorf("ipdb not found")
	}
	var v4Config *service.Config
	var v6Config *service.Config
	var err error
	if v4Path != "" {
		v4Config, err = service.NewV4Config(service.VIndexCache, v4Path, 20)
		if err != nil {
			return nil, err
		}
	}
	if v6Path != "" {
		v6Config, err = service.NewV6Config(service.VIndexCache, v6Path, 20)
		if err != nil {
			return nil, err
		}
	}
	ipdb, err := service.NewIp2Region(v4Config, v6Config)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	if s.ipdb != nil {
		s.ipdb.Close()
	}
	s.ipdb = ipdb
	s.mu.Unlock()
	return ipdb, nil
}

func ipdbPath(fileName string) string {
	if fileName == "" {
		return ""
	}
	dir := filepath.Join(".", ".cache", ipdbDirName)
	path := filepath.Join(dir, fileName)
	if _, err := os.Stat(path); err != nil {
		return ""
	}
	return path
}

func ensureIPDBDir() (string, error) {
	root := filepath.Join(".", ".cache", ipdbDirName)
	if err := os.MkdirAll(root, 0700); err != nil {
		return "", err
	}
	if err := os.Chmod(root, 0700); err != nil {
		return "", err
	}
	return root, nil
}

func downloadFile(ctx context.Context, url, dst string) error {
	url = strings.TrimSpace(url)
	if url == "" {
		return fmt.Errorf("empty url")
	}
	if strings.HasPrefix(url, "file://") {
		local := strings.TrimPrefix(url, "file://")
		data, err := os.ReadFile(local)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, data, 0600)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: clientIpdbTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("unexpected status: %s", resp.Status)
	}
	tmp := dst + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

type ipSketch struct {
	bits []byte
}

func newIPSketch() *ipSketch {
	return &ipSketch{bits: make([]byte, ipSketchBytes)}
}

func decodeIPSketch(encoded string) *ipSketch {
	if encoded == "" {
		return newIPSketch()
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(raw) != ipSketchBytes {
		return newIPSketch()
	}
	return &ipSketch{bits: raw}
}

func (s *ipSketch) Encode() string {
	if s == nil || len(s.bits) == 0 {
		return ""
	}
	return base64.StdEncoding.EncodeToString(s.bits)
}

func (s *ipSketch) Add(ip string) {
	if s == nil {
		return
	}
	idx := hashToIndex(ip)
	byteIdx := idx / 8
	bit := uint(idx % 8)
	if byteIdx >= len(s.bits) {
		return
	}
	s.bits[byteIdx] |= 1 << bit
}

func (s *ipSketch) Estimate() int64 {
	if s == nil || len(s.bits) == 0 {
		return 0
	}
	var ones int64
	for _, b := range s.bits {
		ones += int64(bitsCount(b))
	}
	zeros := int64(ipSketchBits) - ones
	if zeros <= 0 {
		return int64(ipSketchBits)
	}
	estimate := -float64(ipSketchBits) * math.Log(float64(zeros)/float64(ipSketchBits))
	if estimate < 0 {
		return 0
	}
	return int64(estimate + 0.5)
}

func hashToIndex(ip string) int {
	h := fnv.New64a()
	_, _ = h.Write([]byte(ip))
	return int(h.Sum64() % uint64(ipSketchBits))
}

func bitsCount(b byte) int {
	b = (b & 0x55) + ((b >> 1) & 0x55)
	b = (b & 0x33) + ((b >> 2) & 0x33)
	return int((b + (b >> 4)) & 0x0F)
}

type topCounter struct {
	limit int
	items map[string]*ClientIPTop
}

func newTopCounter(limit int) *topCounter {
	if limit <= 0 {
		limit = clientTopLimit
	}
	return &topCounter{
		limit: limit,
		items: make(map[string]*ClientIPTop),
	}
}

func (t *topCounter) Load(items []ClientIPTop) {
	if t == nil {
		return
	}
	for i := range items {
		item := items[i]
		if item.IP == "" || item.Count <= 0 {
			continue
		}
		copied := item
		t.items[item.IP] = &copied
	}
}

func (t *topCounter) Add(ip string, count int64, device, region string) {
	if t == nil || ip == "" || count <= 0 {
		return
	}
	if item, ok := t.items[ip]; ok {
		item.Count += count
		if device != "" {
			item.Device = device
		}
		if region != "" {
			item.Region = region
		}
		return
	}
	if len(t.items) < t.limit {
		t.items[ip] = &ClientIPTop{IP: ip, Count: count, Device: device, Region: region}
		return
	}

	var minKey string
	var minVal *ClientIPTop
	for key, item := range t.items {
		if minVal == nil || item.Count < minVal.Count {
			minKey = key
			minVal = item
		}
	}
	if minVal == nil {
		t.items[ip] = &ClientIPTop{IP: ip, Count: count, Device: device, Region: region}
		return
	}
	delete(t.items, minKey)
	t.items[ip] = &ClientIPTop{
		IP:     ip,
		Count:  minVal.Count + count,
		Device: device,
		Region: region,
	}
}

func (t *topCounter) List() []ClientIPTop {
	if t == nil {
		return nil
	}
	items := make([]ClientIPTop, 0, len(t.items))
	for _, item := range t.items {
		items = append(items, *item)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].Count > items[j].Count
	})
	if len(items) > t.limit {
		items = items[:t.limit]
	}
	return items
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

func dayStartAt(t time.Time) int64 {
	local := t.In(time.Local)
	year, month, day := local.Date()
	start := time.Date(year, month, day, 0, 0, 0, 0, time.Local)
	return start.Unix()
}

func parseDay(val string) (int64, error) {
	val = strings.TrimSpace(val)
	if val == "" {
		return 0, fmt.Errorf("empty date")
	}
	tm, err := time.ParseInLocation("2006-01-02", val, time.Local)
	if err != nil {
		return 0, err
	}
	return dayStartAt(tm), nil
}

func parseHeaders(raw string) map[string][]string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var headers map[string][]string
	if err := json.Unmarshal([]byte(raw), &headers); err == nil {
		return normalizeHeaders(headers)
	}
	var fallback map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &fallback); err != nil {
		return nil
	}
	normalized := make(map[string][]string)
	for k, v := range fallback {
		key := strings.ToLower(strings.TrimSpace(k))
		if key == "" {
			continue
		}
		switch val := v.(type) {
		case string:
			if val != "" {
				normalized[key] = []string{val}
			}
		case []interface{}:
			values := make([]string, 0, len(val))
			for _, item := range val {
				if s, ok := item.(string); ok && s != "" {
					values = append(values, s)
				}
			}
			if len(values) > 0 {
				normalized[key] = values
			}
		}
	}
	return normalized
}

func normalizeHeaders(headers map[string][]string) map[string][]string {
	if len(headers) == 0 {
		return nil
	}
	normalized := make(map[string][]string, len(headers))
	for k, v := range headers {
		key := strings.ToLower(strings.TrimSpace(k))
		if key == "" || len(v) == 0 {
			continue
		}
		normalized[key] = v
	}
	return normalized
}

func firstHeader(headers map[string][]string, key string) string {
	if len(headers) == 0 || key == "" {
		return ""
	}
	values, ok := headers[strings.ToLower(key)]
	if !ok || len(values) == 0 {
		return ""
	}
	return strings.TrimSpace(values[0])
}

func resolveIP(headers map[string][]string, remoteAddr string) string {
	if ip := extractIP(headers, "x-forwarded-for"); ip != "" {
		return ip
	}
	if ip := extractIP(headers, "x-real-ip"); ip != "" {
		return ip
	}
	if ip := extractIP(headers, "remote-host"); ip != "" {
		return ip
	}
	if ip := parseAddrIP(remoteAddr); ip != "" {
		return ip
	}
	return ""
}

func extractIP(headers map[string][]string, key string) string {
	val := firstHeader(headers, key)
	if val == "" {
		return ""
	}
	parts := strings.Split(val, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if ip := normalizeIP(part); ip != "" {
			return ip
		}
	}
	return ""
}

func parseAddrIP(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return normalizeIP(host)
	}
	return normalizeIP(addr)
}

func normalizeIP(ip string) string {
	parsed := net.ParseIP(strings.TrimSpace(ip))
	if parsed == nil {
		return ""
	}
	return parsed.String()
}

func parseDevice(ua string) string {
	ua = strings.TrimSpace(ua)
	if ua == "" {
		return unknownDevice
	}
	lower := strings.ToLower(ua)
	if strings.Contains(lower, "android") {
		model := parseAndroidModel(ua)
		if model != "" {
			return "Android " + model
		}
		return "Android"
	}
	if strings.Contains(lower, "iphone") {
		version := parseIOSVersion(ua)
		if version != "" {
			return "iPhone iOS " + version
		}
		return "iPhone"
	}
	if strings.Contains(lower, "ipad") {
		return "iPad"
	}
	if strings.Contains(lower, "windows") {
		return "Windows"
	}
	if strings.Contains(lower, "mac os x") {
		return "Mac"
	}
	if strings.Contains(lower, "linux") {
		return "Linux"
	}
	if strings.Contains(lower, "bot") || strings.Contains(lower, "spider") {
		return "Bot"
	}
	return "Other"
}

func parseAndroidModel(ua string) string {
	match := androidModelRe.FindStringSubmatch(ua)
	if len(match) > 1 {
		return strings.TrimSpace(match[1])
	}
	return ""
}

func parseIOSVersion(ua string) string {
	match := iosVersionRe.FindStringSubmatch(ua)
	if len(match) > 1 {
		return strings.ReplaceAll(match[1], "_", ".")
	}
	return ""
}

func normalizeRegion(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parts := strings.Split(raw, "|")
	if len(parts) < 3 {
		return ""
	}
	province := strings.TrimSpace(parts[2])
	city := ""
	if len(parts) > 3 {
		city = strings.TrimSpace(parts[3])
	}
	if province == "" || province == "0" {
		province = ""
	}
	if city == "" || city == "0" {
		city = ""
	}
	if province == "" && city == "" {
		return ""
	}
	if province != "" && city == "" {
		return province
	}
	if province == "" && city != "" {
		return city
	}
	return fmt.Sprintf("%s/%s", province, city)
}
