package errstat

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
	unit, err := apix.GetParamStr(ctx, "unit", "day")
	filter, err := apix.GetParamArray[ErrType](ctx, "filter", apix.Force)
	if err != nil {
		return
	}

	resUsage, err := S().TimeRange(ctx, start, end, cache.Granularity(unit), filter...)
	apix.HandleData(ctx, consts.CurdSelectFailCode, resUsage, err)
}
