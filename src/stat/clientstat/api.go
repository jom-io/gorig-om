package clientstat

import (
	"github.com/gin-gonic/gin"
	"github.com/jom-io/gorig/apix"
	"github.com/jom-io/gorig/global/consts"
)

func IPTrend(ctx *gin.Context) {
	defer apix.HandlePanic(ctx)
	start, err := apix.GetParamInt64(ctx, "start", apix.Force)
	end, err := apix.GetParamInt64(ctx, "end", apix.Force)
	if err != nil {
		return
	}
	data, e := S().IPTrend(ctx, start, end)
	apix.HandleData(ctx, consts.CurdSelectFailCode, data, e)
}

func IPTop(ctx *gin.Context) {
	defer apix.HandlePanic(ctx)
	day, err := apix.GetParamForce(ctx, "date")
	limit, err := apix.GetParamInt64(ctx, "limit", apix.NotForce, 20)
	if err != nil {
		return
	}
	data, e := S().IPTop(ctx, day, limit)
	apix.HandleData(ctx, consts.CurdSelectFailCode, data, e)
}

func DeviceDist(ctx *gin.Context) {
	defer apix.HandlePanic(ctx)
	day, err := apix.GetParamForce(ctx, "date")
	if err != nil {
		return
	}
	data, e := S().DeviceDist(ctx, day)
	apix.HandleData(ctx, consts.CurdSelectFailCode, data, e)
}

func RegionDist(ctx *gin.Context) {
	defer apix.HandlePanic(ctx)
	day, err := apix.GetParamForce(ctx, "date")
	if err != nil {
		return
	}
	data, e := S().RegionDist(ctx, day)
	apix.HandleData(ctx, consts.CurdSelectFailCode, data, e)
}

func IPDBInit(ctx *gin.Context) {
	defer apix.HandlePanic(ctx)
	v4URL, err := apix.GetParamStr(ctx, "v4Url")
	if err != nil {
		return
	}
	v6URL, err := apix.GetParamStr(ctx, "v6Url")
	if err != nil {
		return
	}
	e := S().InitIPDB(ctx, v4URL, v6URL)
	apix.HandleData(ctx, consts.CurdUpdateFailCode, gin.H{"ok": e == nil}, e)
}
