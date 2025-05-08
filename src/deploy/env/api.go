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

func CheckGo(ctx *gin.Context) {
	defer apix.HandlePanic(ctx)
	result := Env.CheckGo(ctx)
	apix.HandleData(ctx, consts.CurdSelectFailCode, &result, nil)
}

func InstallGo(ctx *gin.Context) {
	defer apix.HandlePanic(ctx)
	result := Env.InitGo(ctx)
	apix.HandleData(ctx, consts.CurdSelectFailCode, &result, nil)
}

func GoEnvGet(ctx *gin.Context) {
	defer apix.HandlePanic(ctx)
	result := Env.GoEnvGet(ctx)
	apix.HandleData(ctx, consts.CurdSelectFailCode, &result, nil)
}

func GoEnvSet(ctx *gin.Context) {
	defer apix.HandlePanic(ctx)
	envList := &[]GoEnv{}
	e := apix.Bind(ctx, &envList)
	if e != nil {
		return
	}
	err := Env.GoEnvSet(ctx, *envList)
	apix.HandleData(ctx, consts.CurdSelectFailCode, nil, err)
}
