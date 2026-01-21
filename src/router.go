package om

import (
	"github.com/gin-gonic/gin"
	"github.com/jom-io/gorig-om/src/deploy/app"
	dpGit "github.com/jom-io/gorig-om/src/deploy/env"
	dpTask "github.com/jom-io/gorig-om/src/deploy/task"
	"github.com/jom-io/gorig-om/src/host"
	"github.com/jom-io/gorig-om/src/logtool"
	"github.com/jom-io/gorig-om/src/mid"
	"github.com/jom-io/gorig-om/src/omuser"
	"github.com/jom-io/gorig-om/src/stat/apistat"
	"github.com/jom-io/gorig-om/src/stat/errstat"
	"github.com/jom-io/gorig/global/variable"
	"github.com/jom-io/gorig/httpx"
	_ "github.com/tidwall/gjson"
)

func init() {
	Setup()
}

func Setup() {
	if variable.OMKey == "" {
		return
	}
	httpx.RegisterRouter(func(groupRouter *gin.RouterGroup) {
		om := groupRouter.Group("om")
		runApp := om.Group("app")
		runApp.GET("restarted", app.ReStared)
		runApp.Use(mid.Sign())
		runApp.POST("restart", app.Restart)
		runApp.POST("stop", app.Stop)
		runApp.GET("restart/logs", app.RestartLogs)

		auth := om.Group("auth")
		auth.POST("connect", omuser.Login)

		om.Use(mid.Sign())
		log := om.Group("log")
		log.GET("categories", logtool.GetCategories)
		log.GET("levels", logtool.GetLevels)
		log.POST("search", logtool.Search)
		log.GET("near", logtool.Near)
		log.GET("monitor", logtool.Monitor)
		log.GET("download", logtool.Download)

		//git.POST("auto", auto)

		deploy := om.Group("deploy")

		git := deploy.Group("git")
		git.GET("check", dpGit.CheckGit)
		git.POST("install", dpGit.Install)
		deploy.GET("branches", dpGit.Branches)

		goEnv := deploy.Group("go")
		goEnv.GET("check", dpGit.CheckGo)
		goEnv.POST("install", dpGit.InstallGo)
		goEnv.GET("env", dpGit.GoEnvGet)
		goEnv.POST("env", dpGit.GoEnvSet)

		//deploy.GET("repository", dpGit.GetRepo)
		//deploy.POST("repository", dpGit.SetRepo)

		//deploy.POST("branch", dpGit.BranchSet)
		//deploy.GET("branch", dpGit.BranchGet)

		deploy.GET("ssh/key", dpGit.GetSSHKey)
		deploy.POST("ssh/key", dpGit.GenSSHKey)

		task := deploy.Group("task")
		task.GET("config", dpTask.GetConfig)
		task.POST("config", dpTask.SaveConfig)
		task.POST("start", dpTask.Start)
		task.POST("stop", dpTask.Stop)
		task.GET("page", dpTask.Page)
		task.GET("get", dpTask.Get)
		task.POST("rollback", dpTask.Rollback)

		h := om.Group("host")
		h.GET("usage", host.Usage)
		h.GET("usage/time", host.TimeRange)

		e := om.Group("stat")
		e.GET("error/time", errstat.TimeRange)
		e.GET("error/top", errstat.Top)
		//e.GET("api/time", apistat.TimeRange)
		e.GET("api/summary", apistat.Summary)
		e.GET("api/top", apistat.Top)
		e.GET("api/sample", apistat.Sample)
	})
}
