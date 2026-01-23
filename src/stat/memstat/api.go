package memstat

import (
	"github.com/gin-gonic/gin"
	"github.com/jom-io/gorig/apix"
	"github.com/jom-io/gorig/global/consts"
)

func BigTop(ctx *gin.Context) {
	defer apix.HandlePanic(ctx)
	start, err := apix.GetParamInt64(ctx, "start", apix.Force)
	end, err := apix.GetParamInt64(ctx, "end", apix.Force)
	page, err := apix.GetParamInt64(ctx, "page", apix.NotForce, 0)
	size, err := apix.GetParamInt64(ctx, "size", apix.NotForce, 0)
	limit, err := apix.GetParamInt64(ctx, "limit", apix.NotForce, 0)
	sortBy, err := apix.GetParamStr(ctx, "sortBy", "inuseSpace")
	asc, err := apix.GetParamBool(ctx, "asc", false)
	if err != nil {
		return
	}
	if size <= 0 && limit > 0 {
		size = limit
	}
	data, e := S().BigTop(ctx, start, end, page, size, sortBy, asc)
	apix.HandleData(ctx, consts.CurdSelectFailCode, data, e)
}

func BigCount(ctx *gin.Context) {
	defer apix.HandlePanic(ctx)
	start, err := apix.GetParamInt64(ctx, "start", apix.Force)
	end, err := apix.GetParamInt64(ctx, "end", apix.Force)
	if err != nil {
		return
	}
	data, e := S().BigCount(ctx, start, end)
	apix.HandleData(ctx, consts.CurdSelectFailCode, data, e)
}

func LeakLatest(ctx *gin.Context) {
	defer apix.HandlePanic(ctx)
	data, e := S().LeakLatest(ctx)
	if data != nil {
		data.BaseProfile = ""
		data.LeakProfile = ""
	}
	apix.HandleData(ctx, consts.CurdSelectFailCode, data, e)
}

func LeakCount(ctx *gin.Context) {
	defer apix.HandlePanic(ctx)
	start, err := apix.GetParamInt64(ctx, "start", apix.Force)
	end, err := apix.GetParamInt64(ctx, "end", apix.Force)
	if err != nil {
		return
	}
	data, e := S().LeakCount(ctx, start, end)
	apix.HandleData(ctx, consts.CurdSelectFailCode, data, e)
}

func LeakPage(ctx *gin.Context) {
	defer apix.HandlePanic(ctx)
	start, err := apix.GetParamInt64(ctx, "start", apix.NotForce, 0)
	end, err := apix.GetParamInt64(ctx, "end", apix.NotForce, 0)
	page, err := apix.GetParamInt64(ctx, "page", apix.NotForce, 1)
	size, err := apix.GetParamInt64(ctx, "size", apix.NotForce, 10)
	if err != nil {
		return
	}
	data, e := S().LeakPage(ctx, start, end, page, size)
	if data != nil {
		for _, item := range data.Items {
			if item == nil {
				continue
			}
			item.BaseProfile = ""
			item.LeakProfile = ""
		}
	}
	apix.HandleData(ctx, consts.CurdSelectFailCode, data, e)
}
