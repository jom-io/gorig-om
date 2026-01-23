package gorstat

import (
	"github.com/gin-gonic/gin"
	"github.com/jom-io/gorig/apix"
	"github.com/jom-io/gorig/cache"
	"github.com/jom-io/gorig/global/consts"
)

func TimeRange(ctx *gin.Context) {
	defer apix.HandlePanic(ctx)
	start, err := apix.GetParamInt64(ctx, "start", apix.Force)
	end, err := apix.GetParamInt64(ctx, "end", apix.Force)
	unit, err := apix.GetParamStr(ctx, "unit", "minute")
	if err != nil {
		return
	}
	data, e := S().TimeRange(ctx, start, end, cache.Granularity(unit))
	apix.HandleData(ctx, consts.CurdSelectFailCode, data, e)
}
