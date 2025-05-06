package deploy

import (
	"github.com/gin-gonic/gin"
	"github.com/jom-io/gorig/apix"
	"github.com/jom-io/gorig/global/consts"
)

func CheckGit(ctx *gin.Context) {
	defer apix.HandlePanic(ctx)
	result := Env.CheckGit(ctx)
	apix.HandleData(ctx, consts.CurdSelectFailCode, &result, nil)
}

func Install(ctx *gin.Context) {
	defer apix.HandlePanic(ctx)
	result := Env.InstallGit(ctx)
	apix.HandleData(ctx, consts.CurdSelectFailCode, result, nil)
}

func Branches(ctx *gin.Context) {
	defer apix.HandlePanic(ctx)
	repoUrl, e := apix.GetParamType[string](ctx, "repoUrl", apix.Force)
	if e != nil {
		return
	}
	result, err := Env.Branches(ctx, repoUrl)
	apix.HandleData(ctx, consts.CurdSelectFailCode, &result, err)
}

func GetSSHKey(ctx *gin.Context) {
	defer apix.HandlePanic(ctx)
	result := Env.GetSSHKey(ctx)
	apix.HandleData(ctx, consts.CurdSelectFailCode, &result, nil)
}

func GenSSHKey(ctx *gin.Context) {
	defer apix.HandlePanic(ctx)
	result := Env.GenSSHKey(ctx)
	apix.HandleData(ctx, consts.CurdSelectFailCode, &result, nil)
}

func CheckGoEnv(ctx *gin.Context) {
	defer apix.HandlePanic(ctx)
	result := Env.CheckGoEnv(ctx)
	apix.HandleData(ctx, consts.CurdSelectFailCode, &result, nil)
}

func InstallGoEnv(ctx *gin.Context) {
	defer apix.HandlePanic(ctx)
	result := Env.InitGoEnv(ctx)
	apix.HandleData(ctx, consts.CurdSelectFailCode, &result, nil)
}
