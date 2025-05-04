package delpoy

import (
	"github.com/gin-gonic/gin"
	"github.com/jom-io/gorig/apix"
	"github.com/jom-io/gorig/global/consts"
)

func SaveConfig(ctx *gin.Context) {
	defer apix.HandlePanic(ctx)
	opts := &TaskOptions{}
	e := apix.BindParams(ctx, opts)
	if e != nil {
		return
	}

	err := Task.SaveConfig(ctx, *opts)
	apix.HandleData(ctx, consts.CurdSelectFailCode, nil, err)
}

func GetConfig(ctx *gin.Context) {
	defer apix.HandlePanic(ctx)
	config, err := Task.GetConfig(ctx)
	apix.HandleData(ctx, consts.CurdSelectFailCode, config, err)
}

func Start(ctx *gin.Context) {
	defer apix.HandlePanic(ctx)
	err := Task.Start(ctx, false)
	apix.HandleData(ctx, consts.CurdSelectFailCode, nil, err)
}

func Page(ctx *gin.Context) {
	defer apix.HandlePanic(ctx)
	page, e := apix.GetParamType[int64](ctx, "page", apix.Force)
	size, e := apix.GetParamType[int64](ctx, "size", apix.Force)
	if e != nil {
		return
	}
	result, err := Task.Page(ctx, page, size)
	apix.HandleData(ctx, consts.CurdSelectFailCode, result, err)
}

func Get(ctx *gin.Context) {
	defer apix.HandlePanic(ctx)
	id, e := apix.GetParamType[string](ctx, "id", apix.Force)
	if e != nil {
		return
	}
	result, err := Task.Get(ctx, id)
	apix.HandleData(ctx, consts.CurdSelectFailCode, result, err)
}

func Rollback(ctx *gin.Context) {
	defer apix.HandlePanic(ctx)
	id, e := apix.GetParamType[string](ctx, "id", apix.Force)
	if e != nil {
		return
	}
	err := Task.Rollback(ctx, id)
	apix.HandleData(ctx, consts.CurdSelectFailCode, nil, err)
}
