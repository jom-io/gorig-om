package delpoy

import (
	"context"
	ers "errors"
	"fmt"
	"github.com/jom-io/gorig-om/src/deploy"
	"github.com/jom-io/gorig-om/src/deploy/app"
	deployEnv "github.com/jom-io/gorig-om/src/deploy/env"
	"github.com/jom-io/gorig/cache"
	"github.com/jom-io/gorig/cronx"
	"github.com/jom-io/gorig/global/variable"
	"github.com/jom-io/gorig/mid/messagex"
	"github.com/jom-io/gorig/utils/errors"
	"github.com/jom-io/gorig/utils/logger"
	"github.com/jom-io/gorig/utils/sys"
	"github.com/rs/xid"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

var Task taskService

type taskService struct {
}

const DpTaskKey = "dp_task_config"
const workDir = ".deploy"
const TimeOut = 10 * time.Minute

var backupCount = 10

func init() {
	Task = taskService{}
	if variable.OMKey == "" {
		return
	}
	cronx.AddTask("*/10 * * * * *", autoCheck)
	cronx.AddTask("* */1 * * * *", Task.timeOut)
	cronx.AddTask("* */10 * * * *", Task.CleanBackup)
	go Task.run()
	go Task.StartedListen()
}

func SetBackupCount(count int) {
	if count > 0 {
		backupCount = count
	}
}

func (t taskService) SaveConfig(ctx context.Context, opts TaskOptions) *errors.Error {
	logger.Info(ctx, fmt.Sprintf("Saving task config: %v", opts))
	err := cache.New[TaskOptions](cache.Sqlite).Set(DpTaskKey, opts, 0)
	if err != nil {
		return errors.Verify(err.Error())
	}
	return nil
}

func (t taskService) GetConfig(ctx context.Context) (*TaskOptions, *errors.Error) {
	//logger.Info(ctx, "Getting task config")
	opts, err := cache.New[TaskOptions](cache.Sqlite).Get(DpTaskKey)
	if err != nil {
		return nil, errors.Verify(err.Error())
	}
	return &opts, nil
}

func (t taskService) Start(ctx context.Context, auto bool) *errors.Error {
	logger.Info(ctx, "Starting task")
	opts, err := t.GetConfig(ctx)
	if err != nil {
		return err
	}
	if opts == nil {
		return errors.Verify("Task options are nil")
	}
	if opts.Repo == "" || opts.Branch == "" {
		return errors.Verify("Repository URL or branch is empty")
	}
	opts.AutoTrigger = auto
	taskRecord := TaskRecord{
		ID:          xid.New().String(),
		TaskOptions: *opts,
		Commit:      "",
		GitHash:     "",
		CreateAt:    time.Now(),
		Status:      Waiting,
		CreateBy:    "admin",
		BuildFile:   "",
		RBStatus:    UnReady,
	}
	if auto {
		taskRecord.CreateBy = "system"
	}
	if errRun := cache.NewPageStorage[TaskRecord](ctx, cache.Sqlite).Put(taskRecord); errRun != nil {
		return errors.Verify(errRun.Error())
	}

	return nil
}

func (t taskService) Stop(ctx context.Context, id string) *errors.Error {
	logger.Info(ctx, fmt.Sprintf("Stopping task: %s", id))
	cachePage := cache.NewPageStorage[TaskRecord](ctx, cache.Sqlite)
	get, err := cachePage.Get(map[string]any{"id": id})
	if err != nil {
		return errors.Verify(err.Error())
	}
	if get == nil {
		return errors.Verify("Task not found")
	}
	if get.Status != Running && get.Status != Waiting {
		return errors.Verify("Task not running or waiting")
	}
	get.Storage = cachePage
	get.Running(fmt.Sprintf("Stopping task %s", id))
	get.Status = Canceled
	get.Running(fmt.Sprintf("Task %s stopped", id), Warn)
	return nil

}

func (t taskService) Page(ctx context.Context, page, size int64) (*cache.PageCache[TaskRecord], *errors.Error) {
	//logger.Info(ctx, fmt.Sprintf("Getting task page: %d, %d", page, size))
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 10
	}
	_, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cachePage := cache.NewPageStorage[TaskRecord](ctx, cache.Sqlite)
	result, err := cachePage.Find(page, size, nil, cache.PageSorterDesc("createAt"))
	if err != nil {
		return nil, errors.Verify(err.Error())
	}
	//logger.Info(ctx, fmt.Sprintf("Task page result: %v", result))
	return result, nil
}

func (t taskService) Get(ctx context.Context, id string) (*TaskRecord, *errors.Error) {
	cachePage := cache.NewPageStorage[TaskRecord](ctx, cache.Sqlite)
	result, err := cachePage.Get(map[string]any{"id": id})
	if err != nil {
		return nil, errors.Verify(err.Error())
	}
	return result, nil
}

func (t taskService) Rollback(ctx context.Context, id string) *errors.Error {
	logger.Info(ctx, fmt.Sprintf("Rolling back task: %s", id))
	cachePage := cache.NewPageStorage[TaskRecord](ctx, cache.Sqlite)
	get, err := cachePage.Get(map[string]any{"id": id})
	if err != nil {
		return errors.Verify(err.Error())
	}
	if get == nil {
		return errors.Verify("Task not found")
	}
	if get.RBStatus != Ready {
		return errors.Verify("Task not ready for rollback")
	}
	newTask := TaskRecord{
		ID:          xid.New().String(),
		TaskOptions: get.TaskOptions,
		Commit:      get.Commit,
		GitHash:     get.GitHash,
		CreateAt:    time.Now(),
		Status:      Waiting,
		CreateBy:    "admin",
		BuildFile:   get.BuildFile,
		RBStatus:    UnReady,
		RB:          true,
		RID:         id,
	}

	err = cachePage.Put(newTask)
	return nil
}

func autoCheck() {
	ctx := logger.NewCtx()
	defer func() {
		if r := recover(); r != nil {
			logger.Error(ctx, fmt.Sprintf("Panic in autoCheck: %v", r))
		}
	}()
	//logger.Info(ctx, "Auto running task")
	task := Task
	opts, err := task.GetConfig(ctx)
	if err != nil {
		logger.Error(ctx, fmt.Sprintf("Error getting task config: %v", err))
		return
	}
	if opts == nil || !opts.AutoTrigger {
		//logger.Info(ctx, "Auto trigger is disabled or options are nil")
		return
	}
	hash := deployEnv.Env.GetLatestHash(ctx, opts.Repo, opts.Branch)

	storage := cache.NewPageStorage[TaskRecord](ctx, cache.Sqlite)
	item, getErr := storage.Get(map[string]any{"gitHash": hash})
	if getErr != nil {
		logger.Error(ctx, fmt.Sprintf("Error getting task item: %v", getErr))
		return
	}
	if item != nil {
		//logger.Info(ctx, fmt.Sprintf("Task already exists: %s", item.ID))
		return
	}

	if err = task.Start(ctx, true); err != nil {
		logger.Error(ctx, fmt.Sprintf("Error starting task: %v", err))
	}
}

func (t taskService) run() {
	for {
		time.Sleep(5 * time.Second)
		t.deploy(logger.NewCtx())
	}
}

func (t taskService) deploy(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error(ctx, fmt.Sprintf("Panic in deploy: %v", r))
		}
	}()

	var cancel context.CancelFunc
	ctx, cancel = context.WithTimeout(ctx, TimeOut)
	defer cancel()

	storage := cache.NewPageStorage[TaskRecord](ctx, cache.Sqlite)
	// if there are tasks running, do not execute
	runningItems, err := storage.Find(0, 1, map[string]any{"status": Running}, cache.PageSorterAsc("createAt"))
	if err != nil {
		logger.Error(ctx, fmt.Sprintf("Error finding running task items: %v", err))
		return
	}
	if len(runningItems.Items) > 0 {
		//logger.Info(ctx, fmt.Sprintf("There are %d tasks running", len(runningItems.Items)))
		return
	}

	items, err := storage.Find(0, 1, map[string]any{"status": Waiting}, cache.PageSorterAsc("createAt"))
	if err != nil {
		logger.Error(ctx, fmt.Sprintf("Error finding task items: %v", err))
		return
	}

	if len(items.Items) == 0 {
		return
	}
	item := items.Items[0]

	if item.Status == Waiting {
		logger.Info(ctx, fmt.Sprintf("Running task: %s", item.ID))
		topic := fmt.Sprintf("%s.%s", deploy.TopicRunTimeout, logger.GetTraceID(ctx))
		var rID uint64
		rID, _ = messagex.RegisterTopic(topic, func(msg *messagex.Message) *errors.Error {
			item.TimeOut("Deploy timed out")
			return nil
		})

		defer func() {
			_ = messagex.UnSubscribe(topic, rID)
			if ers.Is(ctx.Err(), context.DeadlineExceeded) {
				item.TimeOut("Deploy timed out")
				return
			}
		}()

		item.Ctx = ctx
		item.Storage = storage
		item.Running(fmt.Sprintf("Running task %s", item.ID))

		codeDir := filepath.Join(workDir, "code")
		mainDir := filepath.Join(codeDir, "main")
		if !item.RB {
			item.Running(fmt.Sprintf("Repository: %s, Branch: %s", item.Repo, item.Branch))
			t.clone(ctx, codeDir, mainDir, item)
			defer func() {
				//_ = os.RemoveAll(codeDir)
			}()
		}

		if item.Status != Running {
			return
		}
		runFile := t.buildFile(ctx, mainDir, item)

		if item.Status != Running {
			return
		}
		if restartErr := app.App.Restart(ctx, runFile, func(log string) {
			item.Running(log)
		}, item.ID); restartErr != nil {
			item.Running(restartErr.Error(), Error)
			return
		}
	} else {
		logger.Info(ctx, fmt.Sprintf("Task %s is not in waiting state", item.ID))
	}
}

func (t taskService) clone(ctx context.Context, codeDir, mainDir string, item *TaskRecord) {
	logger.Info(ctx, fmt.Sprintf("Cloning repository: %s", item.Repo))

	item.Running(fmt.Sprintf("Cloning repository: %s, %s", item.Repo, item.Branch), Light)
	if item.Repo == "" || item.Branch == "" {
		item.Running(fmt.Sprintf("Repository URL or branch is empty"))
		return
	}

	item.Running(fmt.Sprintf("Getting latest git hash... "))
	hash := deployEnv.Env.GetLatestHash(ctx, item.Repo, item.Branch)
	item.GitHash = hash
	item.Running(fmt.Sprintf("Git hash: %s", item.GitHash), Light)

	if _, err := os.Stat(codeDir); err == nil {
		item.Running(fmt.Sprintf("Removing existing code directory: %s", codeDir), Warn)
		if err := os.RemoveAll(codeDir); err != nil {
			item.Running(fmt.Sprintf("Error removing code directory: %v", err), Error)
			return
		}
	}

	if err := os.MkdirAll(codeDir, 0755); err != nil {
		item.Running(fmt.Sprintf("Error making code directory: %v", err), Error)
		return
	} else {
		item.Running(fmt.Sprintf("Made code directory: %s", codeDir), Light)
	}

	if err := os.MkdirAll(mainDir, 0755); err != nil {
		item.Running(fmt.Sprintf("Error making main directory: %v", err), Error)
		return
	} else {
		item.Running(fmt.Sprintf("Made main directory: %s", mainDir), Light)
	}

	item.Running(fmt.Sprintf("Cloning repository: %s %s", item.Repo, item.Branch))
	if _, err := deploy.RunCommand(ctx, "git", deploy.DefOpts().SetTimeOut(2*time.Minute), "clone", "--depth", "1", "-b", item.Branch, item.Repo, mainDir); err != nil {
		item.Running(fmt.Sprintf("Error cloning repository: %v", err), Error)
		return
	} else {
		item.Running(fmt.Sprintf("Cloned repository: %s", mainDir), Light)
	}

	if item.OtherRepos != nil && len(*item.OtherRepos) > 0 {
		for _, other := range *item.OtherRepos {
			otherDir := filepath.Join(codeDir, other.Dir)
			if other.Repo == "" || other.Branch == "" {
				continue
			}
			item.Running(fmt.Sprintf("Cloning repository: %s %s", other.Repo, other.Branch))
			if _, err := deploy.RunCommand(ctx, "git", deploy.DefOpts().SetTimeOut(2*time.Minute), "clone", "--depth", "1", "-b", other.Branch, other.Repo, otherDir); err != nil {
				item.Running(fmt.Sprintf("Error cloning repository: %v", err), Error)
				return
			} else {
				item.Running(fmt.Sprintf("Cloned repository: %s", otherDir), Light)
			}
		}
	}

	// commit git log -1 --pretty=%B
	//item.Running(fmt.Sprintf("Getting commit message..."))
	env := []string{
		fmt.Sprintf("GIT_DIR=%s/.git", mainDir),
		fmt.Sprintf("GIT_WORK_TREE=%s", mainDir),
	}
	if commit, err := deploy.RunCommand(ctx, "git", deploy.DefOpts().SetEnv(env), "log", "-1", "--pretty=%B"); err != nil {
		item.Running(fmt.Sprintf("Error getting commit message: %v", err), Error)
		return
	} else {
		item.Commit = commit
		item.Running(fmt.Sprintf("Commit message: %s", item.Commit), Light)
	}
}

func (t taskService) buildFile(ctx context.Context, codeDir string, item *TaskRecord) (runFile string) {
	buildDir := filepath.Join(workDir, "build")
	name := fmt.Sprintf("%s-%s", variable.SysName, sys.RunMode)
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, "_", "-")
	outputName := name + ".linux64"

	if item.RB && item.BuildFile != "" {
		if err := copyFile(item.BuildFile, outputName); err != nil {
			item.Running(fmt.Sprintf("Error copying file: %v", err), Error)
			return
		} else {
			item.Running(fmt.Sprintf("Copied file %s to running directory: %s", item.BuildFile, outputName), Light)
		}
		return outputName
	}

	timestamp := time.Now().Unix()
	backupName := fmt.Sprintf("%s_%d.linux64", name, timestamp)
	item.Running(fmt.Sprintf("Making build directory: %s", buildDir))
	if err := os.MkdirAll(buildDir, 0755); err != nil {
		item.Running(fmt.Sprintf("Error making build directory: %v", err), Error)
		return
	}

	envs := deployEnv.Env.GoEnvGet(ctx)
	proxyConfig := false
	for _, env := range envs {
		if env.Key == "GOPROXY" {
			proxyConfig = true
			break
		}
	}

	if !proxyConfig {
		testURL := "https://proxy.golang.org/github.com/gin-gonic/gin/@v/list"
		client := http.Client{Timeout: 2 * time.Second}

		resp, err := client.Head(testURL)
		useChinaProxy := false
		if err != nil || resp.StatusCode != 200 {
			useChinaProxy = true
		}
		if useChinaProxy {
			envs = append([]deployEnv.GoEnv{
				{
					Key:   "GOPROXY",
					Value: "https://goproxy.cn,direct",
				},
			}, envs...)
			if setErr := deployEnv.Env.GoEnvSet(ctx, envs); setErr != nil {
				item.Running(fmt.Sprintf("Error setting Go environment: %v", setErr), Warn)
			}
		}
	}

	for _, env := range envs {
		if log, err := deploy.RunCommand(ctx, "go", deploy.DefOpts(), "env", "-w", fmt.Sprintf("%s=%s", env.Key, env.Value)); err != nil {
			item.Running(fmt.Sprintf("Error setting Go environment: %v", err), Error)
			return
		} else {
			item.Running(fmt.Sprintf("Set Go environment: env %s=%s %s", env.Key, env.Value, log))
		}
	}

	if _, err := deploy.RunCommand(ctx, "git", deploy.DefOpts(), "config", "--global", "url.git@github.com:.insteadOf", "https://github.com/"); err != nil {
		item.Running(fmt.Sprintf("Error setting git config: %v", err), Error)
		return
	}

	item.Running(fmt.Sprintf("Running go mod tidy..."))

	if _, err := deploy.RunCommand(ctx, "go", deploy.DefOpts().SetDir(codeDir).SetTimeOut(5*time.Minute), "mod", "tidy"); err != nil {
		item.Running(fmt.Sprintf("Error running go mod tidy: %v", err), Error)
		return
	} else {
		item.Running(fmt.Sprintf("Go mod tidy"))
	}

	item.Running(fmt.Sprintf("Finding main.go file..."))
	var mainGoFile string
	if err := filepath.Walk(codeDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() && info.Name() == "main.go" {
			relPath, err := filepath.Rel(codeDir, path)
			if err != nil {
				return err
			}
			mainGoFile = relPath
			item.Running(fmt.Sprintf("Found main.go file: %s", mainGoFile), Light)

			return filepath.SkipDir
		}

		return nil
	}); err != nil {
		item.Running(fmt.Sprintf("Error walking through code directory: %v", err), Error)
	}

	item.Running(fmt.Sprintf("Running go build..."))
	outputPath := filepath.Join(codeDir, outputName)
	//go build -o ${apiBinName}  -ldflags "-w -s"  -trimpath  ./simple/main.go
	buildOpts := &deploy.RunOpts{
		PrintLog: true,
		TimeOut:  2 * time.Minute,
		Dir:      codeDir,
	}
	if _, err := deploy.RunCommand(ctx, "go", buildOpts, "build", "-o", outputName, "-ldflags", "-w -s", "-trimpath", mainGoFile); err != nil {
		item.Running(fmt.Sprintf("Error building file: %v", err), Error)
		return
	} else {
		item.Running(fmt.Sprintf("Build file: %s", outputPath), Light)
	}

	item.Running(fmt.Sprintf("Copying file to running directory..."))
	if err := copyFile(outputPath, outputName); err != nil {
		item.Running(fmt.Sprintf("Error copying file: %v", err), Error)
		return
	} else {
		item.Running(fmt.Sprintf("Copied file to running directory: %s", outputName), Light)
	}

	item.Running(fmt.Sprintf("Copying file to backup directory..."))
	backupPath := filepath.Join(buildDir, backupName)
	if err := copyFile(outputPath, backupPath); err != nil {
		item.Running(fmt.Sprintf("Error copying file to backup directory: %v", err), Error)
		return
	} else {
		item.BuildFile = backupPath
		item.Running(fmt.Sprintf("Copied file to backup directory: %s", backupPath), Light)
	}

	return outputName
}

func (t taskService) StartedListen() {
	var rid uint64
	ctx := logger.NewCtx()
	rid, _ = messagex.RegisterTopic(deploy.TopicRunStarted, func(msg *messagex.Message) *errors.Error {
		id := msg.GetValueStr("itemID")
		logger.Info(ctx, fmt.Sprintf("Task started: %s", id))
		cachePage := cache.NewPageStorage[TaskRecord](ctx, cache.Sqlite)
		item, err := cachePage.Get(map[string]any{"id": id})
		if err != nil {
			logger.Error(ctx, fmt.Sprintf("Error getting task item: %v", err))
			return nil
		}
		logger.Info(ctx, fmt.Sprintf("Task start item: %v", item))
		if item == nil {
			logger.Error(ctx, "Task item not found")
			return nil
		}
		item.Running(fmt.Sprintf("Watchdog service started."))
		item.Running(fmt.Sprintf("Task started: %s", id))
		item.Status = Success
		item.FinishAt = time.Now()
		item.Storage = cachePage
		item.RBStatus = Ready
		item.Running(fmt.Sprintf("Deploy task finished successfully"), Light)

		go func() {
			time.Sleep(100 * time.Millisecond)
			_ = messagex.UnSubscribe(deploy.TopicRunStarted, rid)
		}()
		return nil
	})
}

func (t taskService) timeOut() {
	ctx := logger.NewCtx()
	defer func() {
		if r := recover(); r != nil {
			logger.Error(ctx, fmt.Sprintf("Panic in timeOut: %v", r))
		}
	}()
	storage := cache.NewPageStorage[TaskRecord](ctx, cache.Sqlite)
	items, err := storage.Find(0, 10, map[string]any{"status": Running}, cache.PageSorterAsc("createAt"))
	if err != nil {
		logger.Error(ctx, fmt.Sprintf("Error finding running task items: %v", err))
		return
	}
	if len(items.Items) == 0 {
		return
	}
	for _, item := range items.Items {
		if time.Since(item.StartAt) > TimeOut {
			item.TimeOut("Task timeout")
		}
	}
}

// CleanBackup Only keep the latest backupCount backup files
func (t taskService) CleanBackup() {
	ctx := logger.NewCtx()
	defer func() {
		if r := recover(); r != nil {
			logger.Error(ctx, fmt.Sprintf("Panic in CleanBackup: %v", r))
		}
	}()
	//logger.Info(ctx, fmt.Sprintf("Cleaning backup files"))
	buildDir := filepath.Join(workDir, "build")
	files, err := os.ReadDir(buildDir)
	if err != nil {
		//logger.Error(ctx, fmt.Sprintf("Error reading build directory: %v", err))
		return
	}
	var backupFiles []os.DirEntry
	for _, file := range files {
		if strings.HasSuffix(file.Name(), ".linux64") {
			backupFiles = append(backupFiles, file)
		}
	}

	if len(backupFiles) <= backupCount {
		return
	}

	// Sort files by modification time
	sort.Slice(backupFiles, func(i, j int) bool {
		infoI, _ := backupFiles[i].Info()
		infoJ, _ := backupFiles[j].Info()
		return infoI.ModTime().After(infoJ.ModTime())
	})
	// Remove the oldest files
	for _, file := range backupFiles[backupCount:] {
		filePath := filepath.Join(buildDir, file.Name())
		if err := os.Remove(filePath); err != nil {
			logger.Error(ctx, fmt.Sprintf("Error removing backup file: %v", err))
		} else {
			logger.Info(ctx, fmt.Sprintf("Removed backup file: %s", filePath))
			item, err := cache.NewPageStorage[TaskRecord](ctx, cache.Sqlite).Get(map[string]any{"buildFile": filePath})
			if err != nil {
				logger.Error(ctx, fmt.Sprintf("Error getting task item: %v", err))
			} else {
				item.RBStatus = Cleaned
				item.Running(fmt.Sprintf("Backup file %s removed", filePath), Warn)
				if err := cache.NewPageStorage[TaskRecord](ctx, cache.Sqlite).Update(map[string]any{"id": item.ID}, item); err != nil {
					logger.Error(ctx, fmt.Sprintf("Error updating task item: %v", err))
				}
			}
		}
	}
}

func copyFile(src, dst string) error {
	from, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open src: %w", err)
	}
	defer from.Close()

	info, err := from.Stat()
	if err != nil {
		return fmt.Errorf("stat src: %w", err)
	}

	to, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
	if err == nil {
		defer to.Close()
		_, err = io.Copy(to, from)
		return err
	}

	// if the file is busy, try to rename it
	if !ers.Is(err, syscall.ETXTBSY) {
		return fmt.Errorf("open dst: %w", err)
	}

	tmp := dst + ".tmp"
	tmpFile, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
	if err != nil {
		return fmt.Errorf("create tmp: %w", err)
	}
	defer tmpFile.Close()

	if _, err := io.Copy(tmpFile, from); err != nil {
		return fmt.Errorf("copy to tmp: %w", err)
	}
	if err := tmpFile.Sync(); err != nil {
		return fmt.Errorf("sync tmp: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close tmp: %w", err)
	}

	if err := os.Rename(tmp, dst); err != nil {
		return fmt.Errorf("rename tmp to dst: %w", err)
	}

	return nil
}
