package memstat

import (
	"context"
	stderrors "errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/pprof/profile"
	"github.com/jom-io/gorig/cache"
	"github.com/jom-io/gorig/utils/errors"
	"github.com/jom-io/gorig/utils/logger"
	"go.uber.org/zap"
)

const (
	bigSampleInterval    = 5 * time.Minute
	leakCheckInterval    = 10 * time.Second
	leakGCWindow         = 3
	leakAllocDelta       = 200 * 1024 * 1024
	leakObjectDelta      = 200000
	leakCooldown         = 2 * time.Minute
	bigSampleTopLimit    = 50
	bigMinInuseSpace     = int64(1 << 20)
	leakTopLimit         = 10
	baseProfileKeepCount = 1
	leakProfileKeepCount = 100
	leakProfileMaxAge    = 7 * 24 * time.Hour
	leakEventKeepCount   = 10000
)

var memServ *Serv
var leakTestHold [][]byte

type Serv struct {
	bigStorage  cache.Pager[BigObjStat]
	leakStorage cache.Pager[LeakEvent]

	mu           sync.Mutex
	baseProfile  string
	baseAt       time.Time
	lastLeakAt   time.Time
	lastGC       uint32
	gcSamples    []gcSample
	leakCooldown time.Duration
}

type gcSample struct {
	at          time.Time
	heapAlloc   uint64
	heapObjects uint64
}

func S() *Serv {
	if memServ == nil {
		memServ = &Serv{
			bigStorage:   cache.NewPager[BigObjStat](context.Background(), cache.Sqlite, "mem_big_stat"),
			leakStorage:  cache.NewPager[LeakEvent](context.Background(), cache.Sqlite, "mem_leak_event"),
			leakCooldown: leakCooldown,
		}
	}
	return memServ
}

func init() {
	go S().baselineLoop()
	go S().leakLoop()
	startLeakTest()
}

func (s *Serv) baselineLoop() {
	s.collectBaseline(context.Background())
	ticker := time.NewTicker(bigSampleInterval)
	defer ticker.Stop()
	for range ticker.C {
		s.collectBaseline(context.Background())
	}
}

func (s *Serv) leakLoop() {
	ticker := time.NewTicker(leakCheckInterval)
	defer ticker.Stop()
	for range ticker.C {
		s.checkLeak(context.Background())
	}
}

func (s *Serv) BigTop(ctx context.Context, start, end int64, page, size int64, sortBy string, asc bool) (*cache.PageCache[BigObjRank], *errors.Error) {
	if start == 0 || end == 0 || start > end {
		return nil, errors.Verify("invalid time range")
	}
	if size <= 0 {
		size = 10
	}
	if page <= 0 {
		page = 1
	}

	cond := map[string]any{
		"at": map[string]any{
			"$gte": start,
			"$lte": end,
		},
		"$having": fmt.Sprintf("space >= %d", bigMinInuseSpace),
	}

	groups := []string{"key", "func", "file", "line"}
	aggs := []cache.AggField{
		{Field: "inuseSpace", Agg: cache.AggMax, Alias: "space"},
		{Field: "inuseObjects", Agg: cache.AggMax, Alias: "objs"},
		{Field: "avgObjSize", Agg: cache.AggMax, Alias: "avg"},
		{Field: "at", Agg: cache.AggMax, Alias: "lastAt"},
	}
	sortField := bigSortField(sortBy)
	grouped, err := s.bigStorage.GroupByFields(cond, groups, aggs, page, size, cache.PageSorter{
		SortField: sortField,
		Asc:       asc,
	})
	if err != nil {
		logger.Error(ctx, "GroupByFields big object failed", zap.Error(err))
		return nil, errors.Sys("GroupByFields big object failed", err)
	}

	result := make([]*BigObjRank, 0, len(grouped.Items))
	for _, item := range grouped.Items {
		rank := &BigObjRank{
			Func:         item.Group["func"],
			File:         item.Group["file"],
			InuseSpace:   int64(item.Value["space"]),
			InuseObjects: int64(item.Value["objs"]),
			AvgObjSize:   int64(item.Value["avg"]),
			LastAt:       int64(item.Value["lastAt"]),
		}
		if v := item.Group["line"]; v != "" {
			if line, err := parseInt64(v); err == nil {
				rank.Line = line
			}
		}
		result = append(result, rank)
	}
	return &cache.PageCache[BigObjRank]{
		Total: grouped.Total,
		Page:  grouped.Page,
		Size:  grouped.Size,
		Items: result,
	}, nil
}

func (s *Serv) BigCount(ctx context.Context, start, end int64) (int64, *errors.Error) {
	if start == 0 || end == 0 || start > end {
		return 0, errors.Verify("invalid time range")
	}
	cond := map[string]any{
		"at": map[string]any{
			"$gte": start,
			"$lte": end,
		},
		"$having": fmt.Sprintf("space >= %d", bigMinInuseSpace),
	}
	groups := []string{"func", "file", "line"}
	aggs := []cache.AggField{
		{Field: "inuseSpace", Agg: cache.AggMax, Alias: "space"},
		{Field: "inuseObjects", Agg: cache.AggMax, Alias: "objs"},
	}
	page, err := s.bigStorage.GroupByFields(cond, groups, aggs, 1, 1)
	if err != nil {
		logger.Error(ctx, "GroupByFields big count failed", zap.Error(err))
		return 0, errors.Sys("GroupByFields big count failed", err)
	}
	if page == nil {
		return 0, nil
	}
	return page.Total, nil
}

func (s *Serv) LeakLatest(ctx context.Context) (*LeakEvent, *errors.Error) {
	page, err := s.leakStorage.Find(1, 1, nil, cache.PageSorterDesc("at"))
	if err != nil {
		logger.Error(ctx, "Find leak event failed", zap.Error(err))
		return nil, errors.Sys("Find leak event failed", err)
	}
	if page == nil || len(page.Items) == 0 {
		return nil, nil
	}
	return page.Items[0], nil
}

func (s *Serv) LeakCount(ctx context.Context, start, end int64) (int64, *errors.Error) {
	if start == 0 || end == 0 || start > end {
		return 0, errors.Verify("invalid time range")
	}
	cond := map[string]any{
		"at": map[string]any{
			"$gte": start,
			"$lte": end,
		},
	}
	count, err := s.leakStorage.Count(cond)
	if err != nil {
		logger.Error(ctx, "Count leak event failed", zap.Error(err))
		return 0, errors.Sys("Count leak event failed", err)
	}
	return count, nil
}

func (s *Serv) LeakPage(ctx context.Context, start, end, page, size int64) (*cache.PageCache[LeakEvent], *errors.Error) {
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 10
	}
	cond := map[string]any{}
	if start > 0 || end > 0 {
		if start > 0 && end > 0 && start > end {
			return nil, errors.Verify("invalid time range")
		}
		timeCond := map[string]any{}
		if start > 0 {
			timeCond["$gte"] = start
		}
		if end > 0 {
			timeCond["$lte"] = end
		}
		cond["at"] = timeCond
	}
	if len(cond) == 0 {
		cond = nil
	}
	items, err := s.leakStorage.Find(page, size, cond, cache.PageSorterDesc("at"))
	if err != nil {
		logger.Error(ctx, "Find leak page failed", zap.Error(err))
		return nil, errors.Sys("Find leak page failed", err)
	}
	return items, nil
}

func (s *Serv) checkLeak(ctx context.Context) {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	if ms.NumGC <= s.lastGC {
		return
	}
	s.lastGC = ms.NumGC

	sample := gcSample{
		at:          time.Now(),
		heapAlloc:   ms.HeapAlloc,
		heapObjects: ms.HeapObjects,
	}

	s.mu.Lock()
	s.gcSamples = append(s.gcSamples, sample)
	if len(s.gcSamples) > leakGCWindow {
		s.gcSamples = s.gcSamples[len(s.gcSamples)-leakGCWindow:]
	}
	ready, allocDelta, objDelta := checkLeakWindow(s.gcSamples)
	if !ready || time.Since(s.lastLeakAt) < s.leakCooldown {
		s.mu.Unlock()
		return
	}
	s.lastLeakAt = time.Now()
	s.mu.Unlock()

	s.captureLeak(ctx, ms, allocDelta, objDelta)
}

func checkLeakWindow(samples []gcSample) (bool, uint64, uint64) {
	if len(samples) < leakGCWindow {
		return false, 0, 0
	}
	allocGrowing := true
	objGrowing := true
	for i := 1; i < len(samples); i++ {
		if samples[i].heapAlloc <= samples[i-1].heapAlloc {
			allocGrowing = false
		}
		if samples[i].heapObjects <= samples[i-1].heapObjects {
			objGrowing = false
		}
	}
	allocDelta := samples[len(samples)-1].heapAlloc - samples[0].heapAlloc
	objDelta := samples[len(samples)-1].heapObjects - samples[0].heapObjects
	trigger := (allocGrowing && allocDelta >= leakAllocDelta) || (objGrowing && objDelta >= leakObjectDelta)
	return trigger, allocDelta, objDelta
}

func (s *Serv) collectBaseline(ctx context.Context) {
	logDir, err := ensureLogDir()
	if err != nil {
		logger.Error(ctx, "ensure log dir failed", zap.Error(err))
		return
	}
	path := filepath.Join(logDir, fmt.Sprintf("heap_base_%d.pprof", time.Now().Unix()))
	if err := writeHeapProfile(path); err != nil {
		logger.Error(ctx, "write heap profile failed", zap.Error(err))
		return
	}

	prof, err := loadProfile(path)
	if err != nil {
		logger.Error(ctx, "load heap profile failed", zap.Error(err))
		return
	}

	at := time.Now().Unix()
	s.recordBigStats(ctx, at, prof)

	s.mu.Lock()
	s.baseProfile = path
	s.baseAt = time.Now()
	s.mu.Unlock()

	_ = pruneProfiles(logDir, "heap_base_", baseProfileKeepCount, 0)
	_ = pruneProfiles(logDir, "heap_leak_", leakProfileKeepCount, leakProfileMaxAge)
}

func (s *Serv) recordBigStats(ctx context.Context, at int64, prof *profile.Profile) {
	points, err := profilePoints(prof)
	if err != nil {
		logger.Error(ctx, "parse heap profile failed", zap.Error(err))
		return
	}
	sort.Slice(points, func(i, j int) bool {
		return points[i].inuseSpace > points[j].inuseSpace
	})
	if len(points) > bigSampleTopLimit {
		points = points[:bigSampleTopLimit]
	}
	for _, p := range points {
		if p.inuseSpace <= 0 {
			continue
		}
		if p.inuseSpace < bigMinInuseSpace {
			continue
		}
		stat := BigObjStat{
			At:           at,
			Key:          p.key,
			Func:         p.fn,
			File:         p.file,
			Line:         p.line,
			InuseSpace:   p.inuseSpace,
			InuseObjects: p.inuseObjects,
			AvgObjSize:   p.avgObjSize,
		}
		if err := s.bigStorage.Put(stat); err != nil {
			logger.Error(ctx, "save big object stat failed", zap.Error(err))
		}
	}
}

func (s *Serv) captureLeak(ctx context.Context, ms runtime.MemStats, allocDelta, objDelta uint64) {
	logDir, err := ensureLogDir()
	if err != nil {
		logger.Error(ctx, "ensure log dir failed", zap.Error(err))
		return
	}

	basePath := s.baseProfile
	if basePath == "" || !fileExists(basePath) {
		s.collectBaseline(ctx)
		s.mu.Lock()
		basePath = s.baseProfile
		s.mu.Unlock()
	}

	leakPath := filepath.Join(logDir, fmt.Sprintf("heap_leak_%d.pprof", time.Now().Unix()))
	if err := writeHeapProfile(leakPath); err != nil {
		logger.Error(ctx, "write leak profile failed", zap.Error(err))
		return
	}

	points := make([]LeakPoint, 0)
	var baseProf *profile.Profile
	if basePath != "" && fileExists(basePath) {
		loaded, err := loadProfile(basePath)
		if err != nil {
			logger.Error(ctx, "load base profile failed", zap.Error(err))
		} else {
			baseProf = loaded
		}
	}
	curProf, err := loadProfile(leakPath)
	if err != nil {
		logger.Error(ctx, "load leak profile failed", zap.Error(err))
	}
	if baseProf != nil && curProf != nil {
		points = diffProfilePoints(baseProf, curProf)
	}
	baseSpace, baseObjects := profileTotals(baseProf)
	leakSpace, leakObjects := profileTotals(curProf)
	inuseDelta := leakSpace - baseSpace
	if inuseDelta < 0 {
		inuseDelta = 0
	}
	objectDelta := leakObjects - baseObjects
	if objectDelta < 0 {
		objectDelta = 0
	}
	if len(points) > leakTopLimit {
		points = points[:leakTopLimit]
	}

	event := LeakEvent{
		At:              time.Now().Unix(),
		AllocBytes:      ms.HeapAlloc,
		ObjectCount:     ms.HeapObjects,
		AllocDelta:      uint64(inuseDelta),
		ObjectDelta:     uint64(objectDelta),
		BaseInuseSpace:  baseSpace,
		LeakInuseSpace:  leakSpace,
		BaseInuseObject: baseObjects,
		LeakInuseObject: leakObjects,
		BaseProfile:     basePath,
		LeakProfile:     leakPath,
		Points:          points,
	}
	if err := s.leakStorage.Put(event); err != nil {
		logger.Error(ctx, "save leak event failed", zap.Error(err))
	} else {
		s.pruneLeakEvents(ctx)
	}

	_ = pruneProfiles(logDir, "heap_leak_", leakProfileKeepCount, leakProfileMaxAge)
}

type profilePoint struct {
	key          string
	fn           string
	file         string
	line         int64
	inuseSpace   int64
	inuseObjects int64
	avgObjSize   int64
}

func profilePoints(p *profile.Profile) ([]profilePoint, error) {
	if p == nil {
		return nil, stderrors.New("profile is nil")
	}
	spaceIdx := sampleTypeIndex(p, "inuse_space")
	objIdx := sampleTypeIndex(p, "inuse_objects")
	if spaceIdx < 0 || objIdx < 0 {
		return nil, fmt.Errorf("missing sample types")
	}
	points := make(map[string]*profilePoint)
	for _, s := range p.Sample {
		if len(s.Location) == 0 {
			continue
		}
		loc := s.Location[0]
		if len(loc.Line) == 0 || loc.Line[0].Function == nil {
			continue
		}
		line := loc.Line[0]
		fn := line.Function.Name
		file := line.Function.Filename
		lineNo := int64(line.Line)
		key := fmt.Sprintf("%s|%s|%d", fn, file, lineNo)
		pp := points[key]
		if pp == nil {
			pp = &profilePoint{
				key:  key,
				fn:   fn,
				file: file,
				line: lineNo,
			}
			points[key] = pp
		}
		if spaceIdx < len(s.Value) {
			pp.inuseSpace += s.Value[spaceIdx]
		}
		if objIdx < len(s.Value) {
			pp.inuseObjects += s.Value[objIdx]
		}
	}

	result := make([]profilePoint, 0, len(points))
	for _, p := range points {
		if p.inuseObjects > 0 {
			p.avgObjSize = p.inuseSpace / p.inuseObjects
		} else {
			p.avgObjSize = p.inuseSpace
		}
		result = append(result, *p)
	}
	return result, nil
}

func profileTotals(p *profile.Profile) (int64, int64) {
	if p == nil {
		return 0, 0
	}
	spaceIdx := sampleTypeIndex(p, "inuse_space")
	objIdx := sampleTypeIndex(p, "inuse_objects")
	if spaceIdx < 0 && objIdx < 0 {
		return 0, 0
	}
	var spaceTotal int64
	var objTotal int64
	for _, s := range p.Sample {
		if spaceIdx >= 0 && spaceIdx < len(s.Value) {
			spaceTotal += s.Value[spaceIdx]
		}
		if objIdx >= 0 && objIdx < len(s.Value) {
			objTotal += s.Value[objIdx]
		}
	}
	return spaceTotal, objTotal
}

func diffProfilePoints(base, cur *profile.Profile) []LeakPoint {
	basePoints, _ := profilePoints(base)
	curPoints, _ := profilePoints(cur)
	baseMap := make(map[string]profilePoint, len(basePoints))
	for _, p := range basePoints {
		baseMap[p.key] = p
	}

	diff := make([]LeakPoint, 0)
	for _, curP := range curPoints {
		baseP := baseMap[curP.key]
		deltaSpace := curP.inuseSpace - baseP.inuseSpace
		deltaObjects := curP.inuseObjects - baseP.inuseObjects
		if deltaSpace <= 0 && deltaObjects <= 0 {
			continue
		}
		avgSize := int64(0)
		if deltaObjects > 0 {
			avgSize = deltaSpace / deltaObjects
		} else {
			avgSize = deltaSpace
		}
		diff = append(diff, LeakPoint{
			Func:         curP.fn,
			File:         curP.file,
			Line:         curP.line,
			DeltaSpace:   deltaSpace,
			DeltaObjects: deltaObjects,
			AvgObjSize:   avgSize,
		})
	}

	sort.Slice(diff, func(i, j int) bool {
		return diff[i].DeltaSpace > diff[j].DeltaSpace
	})
	return diff
}

func sampleTypeIndex(p *profile.Profile, name string) int {
	if p == nil {
		return -1
	}
	for i, st := range p.SampleType {
		if strings.EqualFold(st.Type, name) {
			return i
		}
	}
	return -1
}

func loadProfile(path string) (*profile.Profile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return profile.Parse(f)
}

func writeHeapProfile(path string) error {
	if os.Getenv("MEMSTAT_FORCE_GC") == "1" {
		runtime.GC()
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	if err := f.Chmod(0600); err != nil {
		_ = f.Close()
		return err
	}
	defer f.Close()
	return pprof.WriteHeapProfile(f)
}

func ensureLogDir() (string, error) {
	root := filepath.Join(".", ".cache", "heap")
	if err := os.MkdirAll(root, 0700); err != nil {
		return "", err
	}
	if err := os.Chmod(root, 0700); err != nil {
		return "", err
	}
	return root, nil
}

func pruneProfiles(dir, prefix string, keep int, maxAge time.Duration) error {
	if keep <= 0 {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	type fileItem struct {
		name string
		tm   time.Time
	}
	var items []fileItem
	now := time.Now()
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), prefix) || !strings.HasSuffix(entry.Name(), ".pprof") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if maxAge > 0 && now.Sub(info.ModTime()) > maxAge {
			_ = os.Remove(filepath.Join(dir, entry.Name()))
			continue
		}
		items = append(items, fileItem{name: entry.Name(), tm: info.ModTime()})
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].tm.After(items[j].tm)
	})
	if len(items) <= keep {
		return nil
	}
	for _, item := range items[keep:] {
		_ = os.Remove(filepath.Join(dir, item.name))
	}
	return nil
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

func parseInt64(val string) (int64, error) {
	val = strings.TrimSpace(val)
	if val == "" {
		return 0, fmt.Errorf("empty")
	}
	var out int64
	_, err := fmt.Sscanf(val, "%d", &out)
	return out, err
}

func (s *Serv) pruneLeakEvents(ctx context.Context) {
	page, err := s.leakStorage.Find(1, leakEventKeepCount, nil, cache.PageSorterDesc("at"))
	if err != nil {
		logger.Error(ctx, "Find leak events failed", zap.Error(err))
		return
	}
	if page == nil || page.Total <= leakEventKeepCount || len(page.Items) == 0 {
		return
	}
	cutoff := page.Items[len(page.Items)-1].At
	if cutoff == 0 {
		return
	}
	if err := s.leakStorage.Delete(map[string]any{"at": map[string]any{"$lt": cutoff}}); err != nil {
		logger.Error(ctx, "Delete old leak events failed", zap.Error(err))
	}
}

func bigSortField(sortBy string) string {
	switch strings.ToLower(strings.TrimSpace(sortBy)) {
	case "inusespace", "space":
		return "space"
	case "inuseobjects", "objects", "objs":
		return "objs"
	case "avgobjsize", "avg":
		return "avg"
	case "lastat", "at":
		return "lastAt"
	default:
		return "space"
	}
}

func startLeakTest() {
	if os.Getenv("MEMSTAT_LEAK_TEST") != "1" {
		return
	}
	sizeMB := parseEnvInt("MEMSTAT_LEAK_MB", 20)
	count := parseEnvInt("MEMSTAT_LEAK_COUNT", 15)
	interval := parseEnvDuration("MEMSTAT_LEAK_INTERVAL", 2*time.Second)
	forceGC := os.Getenv("MEMSTAT_LEAK_FORCE_GC") == "1"
	if sizeMB <= 0 || count <= 0 {
		return
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for i := 0; i < count; i++ {
			<-ticker.C
			buf := make([]byte, sizeMB*1024*1024)
			for idx := 0; idx < len(buf); idx += 4096 {
				buf[idx] = 1
			}
			leakTestHold = append(leakTestHold, buf)
			if forceGC {
				runtime.GC()
			}
		}
	}()
}

func parseEnvInt(key string, def int) int {
	val := strings.TrimSpace(os.Getenv(key))
	if val == "" {
		return def
	}
	parsed, err := strconv.Atoi(val)
	if err != nil {
		return def
	}
	return parsed
}

func parseEnvDuration(key string, def time.Duration) time.Duration {
	val := strings.TrimSpace(os.Getenv(key))
	if val == "" {
		return def
	}
	parsed, err := time.ParseDuration(val)
	if err != nil {
		return def
	}
	return parsed
}
