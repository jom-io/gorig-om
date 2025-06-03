package om

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jom-io/gorig-om/src/host"
	"github.com/jom-io/gorig/cache"
	"testing"
	"time"
)

func TestHostCollect(t *testing.T) {
	ctx := context.Background()

	const interval = 5 * time.Second
	const rounds = 3

	for i := 0; i < rounds; i++ {
		fmt.Printf("\n====== Round %d: %s ======\n", i+1, time.Now().Format(time.RFC3339))

		// execute the host resource collection
		host.Host().Collect(ctx)

		// wait for a short period to allow the collection to complete
		items, err := host.Host().GetHostResourceUsage(ctx, 1, 1)
		if err != nil {
			t.Errorf("Failed to get usage after collect: %v", err)
		} else if items != nil && len(items.Items) > 0 {
			usage := items.Items[0]
			fmt.Printf("Latest usage:\n"+
				"  App CPU:     %s%%\n"+
				"  App Memory:  %s MB\n"+
				"  App Disk:    %s MB\n"+
				"  Host CPU:    %s%%\n"+
				"  Host Memory: %s / %s MB\n"+
				"  Host Disk:   %s / %s MB\n"+
				"  Timestamp:   %d\n",
				usage.AppCpu, usage.AppMem, usage.AppDisk,
				usage.CPU, usage.Mem, usage.TotalMem,
				usage.Disk, usage.TotalDisk, usage.At)
		} else {
			fmt.Println("No usage data found.")
		}

		time.Sleep(interval)
	}
}

func TestHostClear(t *testing.T) {
	ctx := context.Background()

	// Set short expiration time for test
	host.SetMaxPeriod(2 * time.Second)

	// Step 1: Collect first usage (this one should be expired later)
	host.Host().Collect(ctx)
	time.Sleep(3 * time.Second) // ensure it gets expired

	// Step 2: Collect second usage (should remain)
	host.Host().Collect(ctx)

	// Step 3: Verify 2 items exist before clear
	beforeClear, err := host.Host().GetHostResourceUsage(ctx, 1, 10)
	if err != nil {
		t.Fatalf("Failed to get usage before clear: %v", err)
	}
	if beforeClear == nil || len(beforeClear.Items) < 2 {
		t.Fatalf("Expected at least 2 usage items before clear, got: %v", beforeClear)
	}
	t.Logf("Before clear: %d items", len(beforeClear.Items))

	// Step 4: Clear expired
	if err := host.Host().ClearHostResourceUsage(ctx); err != nil {
		t.Errorf("ClearHostResourceUsage failed: %v", err)
	}

	// Step 5: Fetch again and expect only one item left
	afterClear, err := host.Host().GetHostResourceUsage(ctx, 1, 10)
	if err != nil {
		t.Fatalf("Failed to get usage after clear: %v", err)
	}
	if afterClear == nil || len(afterClear.Items) != 1 {
		t.Errorf("Expected 1 usage item after clear, got: %v", afterClear)
	} else {
		t.Logf("After clear: only latest item kept as expected, time=%d", afterClear.Items[0].At)
	}
}

func TestHostTimeRange(t *testing.T) {
	ctx := context.Background()
	host.Host().Collect(ctx)
	host.Host().Collect(ctx)
	start := time.Now().Add(-1 * time.Hour).Unix()
	end := time.Now().Unix()
	unit := cache.GranularityHour

	resUsage, err := host.Host().TimeRange(ctx, start, end, unit, host.ResTypeCPU, host.ResTypeMemory, host.ResTypeDisk)
	if err != nil {
		t.Fatalf("Failed to get time range usage: %v", err)
	}

	if len(resUsage) == 0 {
		t.Errorf("Expected non-empty usage data for field '%s' between %d and %d", host.ResTypeCPU, start, end)
	} else {
		jsonData, _ := json.Marshal(resUsage)
		t.Logf("Time range usage for field '%s': %v", host.ResTypeCPU, string(jsonData))
	}

	time.Sleep(12 * time.Second) // wait for any async operations to complete
}
