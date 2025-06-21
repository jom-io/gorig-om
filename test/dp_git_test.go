package test

import (
	deploy "github.com/jom-io/gorig-om/src/deploy/env"
	"github.com/jom-io/gorig/utils/logger"
	"testing"
)

func TestCheckVersion(t *testing.T) {
	ctx := logger.NewCtx()

	git := deploy.Env.CheckGit(ctx)
	t.Log(git)
}

// GetSSHKey
func TestGetSSHKey(t *testing.T) {
	ctx := logger.NewCtx()
	sshKey := deploy.Env.GetSSHKey(ctx)
	t.Logf("SSH Key: %s", sshKey)
}

// ListBranches
func TestListBranches(t *testing.T) {
	ctx := logger.NewCtx()
	repoUrl := ""
	if branches, e := deploy.Env.Branches(ctx, repoUrl); e != nil {
		t.Errorf("Error: %v", e)
		return
	} else if len(branches) == 0 {
		t.Log("No branches found")
		return
	} else {
		t.Logf("Branches: %v", branches)
		return
	}
}
