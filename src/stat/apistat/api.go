package apistat

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
	unit, err := apix.GetParamStr(ctx, "unit", "hour")
	filter, err := apix.GetParamArray[ApiStatType](ctx, "filter", apix.NotForce)
	if err != nil {
		return
	}

	data, e := S().TimeRange(ctx, start, end, cache.Granularity(unit), filter...)
	apix.HandleData(ctx, consts.CurdSelectFailCode, data, e)
}

func Summary(ctx *gin.Context) {
	defer apix.HandlePanic(ctx)
	start, err := apix.GetParamInt64(ctx, "start", apix.Force)
	end, err := apix.GetParamInt64(ctx, "end", apix.Force)
	slowMs, err := apix.GetParamInt64(ctx, "slowMs", apix.NotForce, 200)
	if err != nil {
		return
	}

	data, e := S().Summary(ctx, start, end, slowMs)
	apix.HandleData(ctx, consts.CurdSelectFailCode, data, e)
}

func Top(ctx *gin.Context) {
	defer apix.HandlePanic(ctx)
	start, err := apix.GetParamInt64(ctx, "start", apix.Force)
	end, err := apix.GetParamInt64(ctx, "end", apix.Force)
	pageReq, err := apix.GetPageReq(ctx)
	methods, err := apix.GetParamArray[string](ctx, "methods", apix.NotForce)
	negMethods, err := apix.GetParamArray[string](ctx, "negMethods", apix.NotForce)
	uriPrefix, err := apix.GetParamStr(ctx, "uriPrefix")
	statuses, err := apix.GetParamArray[string](ctx, "statuses", apix.NotForce)
	sortBy, err := apix.GetParamStr(ctx, "sortBy", "avg")
	asc, err := apix.GetParamBool(ctx, "asc", false)
	if err != nil {
		return
	}

	data, e := S().TopPage(ctx, start, end, pageReq.Page, pageReq.Size, methods, negMethods, uriPrefix, statuses, sortBy, asc)
	apix.HandleData(ctx, consts.CurdSelectFailCode, data, e)
}

func Sample(ctx *gin.Context) {
	defer apix.HandlePanic(ctx)
	method, err := apix.GetParamForce(ctx, "method")
	uri, err := apix.GetParamForce(ctx, "uri")
	types, err := apix.GetParamArray[string](ctx, "types", apix.NotForce)
	if err != nil {
		return
	}

	data, e := S().Sample(ctx, method, uri, types)
	apix.HandleData(ctx, consts.CurdSelectFailCode, data, e)
}
