package delpoy

import (
	"context"
	"fmt"
	"github.com/jom-io/gorig-om/src/deploy"
	"github.com/jom-io/gorig-om/src/deploy/app"
	delpoy "github.com/jom-io/gorig-om/src/deploy/env"
	"github.com/jom-io/gorig/cache"
	"github.com/jom-io/gorig/cronx"
	"github.com/jom-io/gorig/global/variable"
	"github.com/jom-io/gorig/utils/errors"
	"github.com/jom-io/gorig/utils/logger"
	"github.com/jom-io/gorig/utils/sys"
	"github.com/rs/xid"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var Task taskService

type taskService struct {
}

const DpTaskKey = "dp_task_config"
const workDir = ".deploy"
const TimeOut = 3 * time.Minute

var backupCount = 10

func init() {
	Task = taskService{}
	cronx.AddTask("*/10 * * * * *", autoCheck)
	cronx.AddTask("*/15 * * * * *", Task.timeOut)
	cronx.AddTask("* */10 * * * *", Task.CleanBackup)
	go Task.run()
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

func (t taskService) Page(ctx context.Context, page, size int64) (*cache.PageCache[TaskRecord], *errors.Error) {
	//logger.Info(ctx, fmt.Sprintf("Getting task page: %d, %d", page, size))
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 10
	}
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
	hash := delpoy.Env.GetLatestHash(ctx, opts.Repo, opts.Branch)

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

	//err = task.Start(ctx, true)
	if err != nil {
		logger.Error(ctx, fmt.Sprintf("Error starting task: %v", err))
	}
}

func (t taskService) run() {
	time.Sleep(10 * time.Second)
	t.deploy(logger.NewCtx())
	t.run()
}

func (t taskService) deploy(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error(ctx, fmt.Sprintf("Panic in deploy: %v", r))
		}
	}()
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

	items, err := storage.Find(0, 10, map[string]any{"status": Waiting}, cache.PageSorterAsc("createAt"))
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
		item.Ctx = ctx
		item.Storage = storage
		item.Running(fmt.Sprintf("Running task %s", item.ID))
		item.Running(fmt.Sprintf("Repository: %s, Branch: %s", item.Repo, item.Branch))

		codeDir := filepath.Join(workDir, "code")
		t.clone(ctx, codeDir, item)
		defer func() {
			_ = os.RemoveAll(codeDir)
		}()
		if item.Status != Running {
			return
		}
		runFile := t.buildFile(ctx, codeDir, item)

		if item.Status != Running {
			return
		}
		if restartErr := app.App.Restart(ctx, runFile, func(log string) {
			item.Running(log)
		}); restartErr != nil {
			item.Running(restartErr.Error(), Error)
			return
		}

		item.Status = Success
		item.FinishAt = time.Now()
		item.Running(fmt.Sprintf("Deploy task finished successfully"), Light)
	} else {
		logger.Info(ctx, fmt.Sprintf("Task %s is not in waiting state", item.ID))
	}
}

func (t taskService) clone(ctx context.Context, codeDir string, item *TaskRecord) {
	logger.Info(ctx, fmt.Sprintf("Cloning repository: %s", item.Repo))
	item.Running(fmt.Sprintf("Cloning repository: %s, %s", item.Repo, item.Branch), Light)

	if item.Repo == "" || item.Branch == "" {
		item.Running(fmt.Sprintf("Repository URL or branch is empty"))
		return
	}

	item.Running(fmt.Sprintf("Getting latest git hash... "))
	hash := delpoy.Env.GetLatestHash(ctx, item.Repo, item.Branch)
	item.GitHash = hash
	item.Running(fmt.Sprintf("Git hash: %s", item.GitHash), Light)

	item.Running(fmt.Sprintf("Making code directory: %s", codeDir))
	if _, err := os.Stat(codeDir); err == nil {
		item.Running(fmt.Sprintf("Removing existing code directory: %s", codeDir), Warn)
		if err := os.RemoveAll(codeDir); err != nil {
			item.Running(fmt.Sprintf("Error removing code directory: %v", err), Error)
			return
		}
	}

	item.Running(fmt.Sprintf("Cloning repository: %s %s", item.Repo, item.Branch))
	if _, err := deploy.RunCommand(ctx, "git", "clone", "--depth", "1", "-b", item.Branch, item.Repo, codeDir); err != nil {
		item.Running(fmt.Sprintf("Error cloning repository: %v", err), Error)
		return
	} else {
		item.Running(fmt.Sprintf("Cloned repository: %s", codeDir), Light)
	}

	// commit git log -1 --pretty=%B
	//item.Running(fmt.Sprintf("Getting commit message..."))
	env := []string{
		fmt.Sprintf("GIT_DIR=%s/.git", codeDir),
		fmt.Sprintf("GIT_WORK_TREE=%s", codeDir),
	}
	if commit, err := deploy.RunCommandEnv(ctx, env, "git", "log", "-1", "--pretty=%B"); err != nil {
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

	envs := []struct {
		key, value string
	}{
		{"GOARCH", "amd64"},
		{"GOOS", "linux"},
		{"CGO_ENABLED", "0"},
		{"GO111MODULE", "on"},
	}
	for _, env := range envs {
		if log, err := deploy.RunCommandDir(ctx, codeDir, "go", "env", "-w", fmt.Sprintf("%s=%s", env.key, env.value)); err != nil {
			item.Running(fmt.Sprintf("Error setting Go environment: %v", err), Error)
			return
		} else {
			item.Running(fmt.Sprintf("Set Go environment: env %s=%s, %s", env.key, env.value, log))
		}
	}

	item.Running(fmt.Sprintf("Running go mod tidy..."))
	if _, err := deploy.RunCommandDir(ctx, codeDir, "go", "mod", "tidy"); err != nil {
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
	if _, err := deploy.RunCommandDir(ctx, codeDir, "go", "build", "-o", outputName, "-ldflags", "-w -s", "-trimpath", mainGoFile); err != nil {
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
		item.RBStatus = Ready
		item.Running(fmt.Sprintf("Copied file to backup directory: %s", backupPath), Light)
	}

	//item.Running(fmt.Sprintf("Cleaning code directory: %s", codeDir))
	//if err := os.RemoveAll(codeDir); err != nil {
	//	item.Running(fmt.Sprintf("Error cleaning code directory: %v", err), Error)
	//	return
	//} else {
	//	item.Running(fmt.Sprintf("Cleaned code directory: %s", codeDir))
	//}
	return outputName
}

func (t taskService) timeOut() {
	ctx := logger.NewCtx()
	defer func() {
		if r := recover(); r != nil {
			logger.Error(ctx, fmt.Sprintf("Panic in timeOut: %v", r))
		}
	}()
	storage := cache.NewPageStorage[TaskRecord](ctx, cache.Sqlite)
	items, err := storage.Find(0, 1, map[string]any{"status": Running}, cache.PageSorterAsc("createAt"))
	if err != nil {
		logger.Error(ctx, fmt.Sprintf("Error finding running task items: %v", err))
		return
	}
	if len(items.Items) == 0 {
		return
	}
	for _, item := range items.Items {
		if time.Since(item.StartAt) > TimeOut {
			item.Running(fmt.Sprintf("Task timeout, Cancelled"), Warn)
			item.Status = Timeout
			item.FinishAt = time.Now()
			_ = storage.Update(map[string]any{"id": item.ID}, item)
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
		return err
	}
	defer from.Close()

	to, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer to.Close()

	_, err = from.Stat()
	if err != nil {
		return err
	}

	_, err = from.Seek(0, 0)
	if err != nil {
		return err
	}

	_, err = to.ReadFrom(from)
	return err
}
