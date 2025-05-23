package app

import (
	"github.com/gin-gonic/gin"
	"github.com/jom-io/gorig/apix"
	"github.com/jom-io/gorig/global/consts"
)

func Restart(ctx *gin.Context) {
	defer apix.HandlePanic(ctx)
	err := App.Restart(ctx, "", nil)
	apix.HandleData(ctx, consts.CurdSelectFailCode, nil, err)
}

func ReStared(ctx *gin.Context) {
	defer apix.HandlePanic(ctx)
	startID, e := apix.GetParamForce(ctx, "startID")
	itemID, e := apix.GetParamStr(ctx, "itemID")
	if e != nil {
		return
	}
	App.RestartSuccess(ctx, startID, itemID)
	apix.HandleData(ctx, consts.CurdSelectFailCode, nil, nil)
}

func Stop(ctx *gin.Context) {
	defer apix.HandlePanic(ctx)
	err := App.Stop(ctx)
	apix.HandleData(ctx, consts.CurdSelectFailCode, nil, err)
}
