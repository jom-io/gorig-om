package apistat

import (
	"github.com/gin-gonic/gin"
	"github.com/jom-io/gorig/apix"
	"github.com/jom-io/gorig/global/consts"
)

func Top(ctx *gin.Context) {
	defer apix.HandlePanic(ctx)
	start, err := apix.GetParamInt64(ctx, "start", apix.Force)
	end, err := apix.GetParamInt64(ctx, "end", apix.Force)
	limit, err := apix.GetParamInt64(ctx, "limit", 10)
	methods, err := apix.GetParamArray[string](ctx, "methods")
	uriPrefix, err := apix.GetParamStr(ctx, "uriPrefix")
	statuses, err := apix.GetParamArray[string](ctx, "statuses")
	sortBy, err := apix.GetParamStr(ctx, "sortBy", "avg")
	asc, err := apix.GetParamBool(ctx, "asc", false)
	if err != nil {
		return
	}

	data, e := S().Top(ctx, start, end, methods, uriPrefix, statuses, sortBy, asc, limit)
	apix.HandleData(ctx, consts.CurdSelectFailCode, data, e)
}
