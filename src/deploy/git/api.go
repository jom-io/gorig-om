package deploy

import (
	"github.com/gin-gonic/gin"
	"github.com/jom-io/gorig/apix"
	"github.com/jom-io/gorig/global/consts"
)

func CheckGit(ctx *gin.Context) {
	defer apix.HandlePanic(ctx)
	result := Git.CheckGit(ctx)
	apix.HandleData(ctx, consts.CurdSelectFailCode, &result, nil)
}

func Install(ctx *gin.Context) {
	defer apix.HandlePanic(ctx)
	result := Git.InstallGit(ctx)
	apix.HandleData(ctx, consts.CurdSelectFailCode, result, nil)
}

func Branches(ctx *gin.Context) {
	defer apix.HandlePanic(ctx)
	repoUrl, e := apix.GetParamType[string](ctx, "repoUrl", apix.Force)
	if e != nil {
		return
	}
	result, err := Git.Branches(ctx, repoUrl)
	apix.HandleData(ctx, consts.CurdSelectFailCode, &result, err)
}

func GetSSHKey(ctx *gin.Context) {
	defer apix.HandlePanic(ctx)
	result := Git.GetSSHKey(ctx)
	apix.HandleData(ctx, consts.CurdSelectFailCode, &result, nil)
}

func GenSSHKey(ctx *gin.Context) {
	defer apix.HandlePanic(ctx)
	result := Git.GenSSHKey(ctx)
	apix.HandleData(ctx, consts.CurdSelectFailCode, &result, nil)
}
