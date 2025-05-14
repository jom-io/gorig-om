package om

import (
	deploy "github.com/jom-io/gorig-om/src/deploy/app"
	"github.com/jom-io/gorig/utils/logger"
	"testing"
)

func TestRestartApp(t *testing.T) {
	ctx := logger.NewCtx()

	defer func() {
		if e := deploy.App.Clean(ctx); e != nil {
			t.Errorf("Error: %v", e)
			return
		}
	}()

	if e := deploy.App.Restart(ctx, "", nil); e != nil {
		t.Errorf("Error: %v", e)
		return
	}

}

func TestStopApp(t *testing.T) {
	ctx := logger.NewCtx()

	defer func() {
		if e := deploy.App.Clean(ctx); e != nil {
			t.Errorf("Error: %v", e)
			return
		}
	}()

	if e := deploy.App.Stop(ctx); e != nil {
		t.Errorf("Error: %v", e)
		return
	}
}
