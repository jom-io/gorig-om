package host

import (
	"context"
	"fmt"
	"github.com/jom-io/gorig/cache"
	"github.com/jom-io/gorig/cronx"
	configure "github.com/jom-io/gorig/utils/cofigure"
	"github.com/jom-io/gorig/utils/errors"
	"github.com/jom-io/gorig/utils/logger"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/process"
	"go.uber.org/zap"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var host *Serv

type Serv struct {
	storage cache.Pager[ResUsage]
}

var (
	maxPeriod = 30 * 24 * time.Hour
)

func Host() *Serv {
	if host == nil {
		return &Serv{
			storage: cache.NewPager[ResUsage](context.Background(), cache.Sqlite),
		}
	}
	return host
}

func init() {

	getString := configure.GetString("om.host.max_period", "720h")
	if len(getString) > 0 {
		if getMaxPeriod, err := time.ParseDuration(getString); err == nil {
			maxPeriod = getMaxPeriod
		} else {
			logger.Error(context.Background(), "Failed to parse MaxPeriod", zap.String("value", getString), zap.Error(err))
		}
	}

	// every minute collect host resource usage
	cronx.AddCronTask("0 * * * * *", Host().Collect, 10*time.Second)
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := Host().ClearHostResourceUsage(context.Background()); err != nil {
					logger.Error(context.Background(), "ClearHostResourceUsage failed", zap.Error(err))
				}
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
	logger.Info(context.Background(), "MaxPeriod set", zap.Duration("maxPeriod", maxPeriod))
}

func (s *Serv) Collect(ctx context.Context) {
	pid := int32(os.Getpid())
	proc, err := process.NewProcess(pid)
	if err != nil {
		logger.Error(ctx, "Collect failed to get process", zap.Error(err))
		return
	}

	var wg sync.WaitGroup
	var hostCPUPercent []float64
	var hostCPUErr error
	var appCPUPercent float64
	var appCPUErr error

	wg.Add(2)

	go func() {
		defer wg.Done()
		hostCPUPercent, hostCPUErr = cpu.Percent(time.Second, false)
	}()

	go func() {
		defer wg.Done()
		appCPUPercent, appCPUErr = proc.Percent(time.Second)
	}()

	wg.Wait()

	if hostCPUErr != nil {
		logger.Error(ctx, "Collect failed to get host CPU percent", zap.Error(hostCPUErr))
		return
	}

	var hostCpuAvg float64 = 0
	if len(hostCPUPercent) > 0 {
		var sum float64
		for _, v := range hostCPUPercent {
			sum += v
		}
		hostCpuAvg = sum / float64(len(hostCPUPercent))
	}

	if appCPUErr != nil {
		logger.Error(ctx, "Collect failed to get app CPU percent", zap.Error(appCPUErr))
		return
	}

	vm, err := mem.VirtualMemory()
	if err != nil {
		logger.Error(ctx, "Collect failed to get virtual memory", zap.Error(err))
		return
	}

	rss, err := proc.MemoryInfo()
	if err != nil {
		logger.Error(ctx, "Collect failed to get process memory info", zap.Error(err))
		return
	}

	diskUsage, err := disk.Usage("/")
	if err != nil {
		logger.Error(ctx, "Collect failed to get root disk usage", zap.Error(err))
		return
	}

	var dirUsed uint64 = 0
	currentDir, err := os.Getwd()
	if err != nil {
		logger.Error(ctx, "Collect failed to get current directory", zap.Error(err))
		return
	}
	dirUsed += s.getDiskUsage(currentDir)

	resUsage := ResUsage{
		AppCpu:    fmt.Sprintf("%.2f", appCPUPercent),                      // Application CPU usage in percentage
		AppMem:    fmt.Sprintf("%.2f", float64(rss.RSS)/1024/1024),         // Application Memory usage in MB
		AppDisk:   fmt.Sprintf("%.2f", float64(dirUsed)/1024/1024),         // Application Disk usage in MB
		CPU:       fmt.Sprintf("%.2f", hostCpuAvg),                         // Host CPU usage in percentage
		Mem:       fmt.Sprintf("%.2f", float64(vm.Used)/1024/1024),         // Host Memory usage in MB
		TotalMem:  fmt.Sprintf("%.2f", float64(vm.Total)/1024/1024),        // Total Memory in MB
		Disk:      fmt.Sprintf("%.2f", float64(diskUsage.Used)/1024/1024),  // Disk usage in MB
		TotalDisk: fmt.Sprintf("%.2f", float64(diskUsage.Total)/1024/1024), // Total Disk in MB
		At:        time.Now().Unix(),
	}
	if err = s.storage.Put(resUsage); err != nil {
		logger.Error(ctx, "Collect failed to save resource usage", zap.Error(err))
		return
	}
}

func (s *Serv) GetHostResourceUsage(ctx context.Context, page, size int64) (*cache.PageCache[ResUsage], error) {
	items, err := s.storage.Find(page, size, nil, cache.PageSorterDesc("at"))
	if err != nil {
		logger.Error(ctx, "GetHostResourceUsage failed", zap.Error(err))
		return nil, err
	}
	return items, nil
}

// ClearHostResourceUsage clears the host resource usage data older than MaxPeriod.
func (s *Serv) ClearHostResourceUsage(ctx context.Context) error {
	expirationTime := time.Now().Add(-maxPeriod).Unix()
	if err := s.storage.Delete(map[string]any{"time": map[string]any{"$lt": expirationTime}}); err != nil {
		logger.Error(ctx, "ClearHostResourceUsage failed", zap.Error(err))
		return err
	}

	return nil
}

func (s *Serv) getDiskUsage(path string) uint64 {
	var totalSize uint64 = 0
	err := filepath.Walk(path, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			logger.Error(context.Background(), "Walk error", zap.String("path", path), zap.Error(err))
			return nil
		}
		if !info.IsDir() {
			totalSize += uint64(info.Size())
		}
		return nil
	})
	if err != nil {
		logger.Error(context.Background(), "Walk failed", zap.String("path", path), zap.Error(err))
	}
	return totalSize
}

func (s *Serv) Page(ctx context.Context, page, size int64) (*cache.PageCache[ResUsage], *errors.Error) {
	logger.Info(ctx, "Host usage page called", zap.Int64("page", page), zap.Int64("size", size))
	items, err := s.storage.Find(page, size, nil, cache.PageSorterDesc("at"))
	if err != nil {
		return nil, errors.Verify("FindByPage failed", err)
	}
	return items, nil
}

func (s *Serv) TimeRange(ctx context.Context, start, end int64, granularity cache.Granularity, field ...ResType) ([]*cache.PageTimeItem, *errors.Error) {
	from := time.Unix(start, 0)
	to := time.Unix(end, 0)
	logger.Info(ctx, "Host usage time range called", zap.Time("from", from), zap.Time("to", to), zap.Any("granularity", granularity), zap.Any("field", field))
	if from.IsZero() || to.IsZero() || from.After(to) {
		return nil, errors.Verify("Invalid time range")
	}
	if granularity == "" {
		granularity = cache.GranularityHour
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
