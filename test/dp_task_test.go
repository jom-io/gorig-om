package test

import (
	delpoy "github.com/jom-io/gorig-om/src/deploy/task"
	"github.com/jom-io/gorig/utils/logger"
	"testing"
)

func TestSaveTask(t *testing.T) {
	ctx := logger.NewCtx()

	if e := delpoy.Task.SaveConfig(ctx, delpoy.TaskOptions{
		Repo:   "git@github.com-jom:jom-io/gorig.git",
		Branch: "test",
	}); e != nil {
		t.Errorf("Error: %v", e)
		return
	}
}
