package delpoy

import (
	"context"
	"fmt"
	"github.com/jom-io/gorig/cache"
	"github.com/jom-io/gorig/utils/logger"
	"time"
)

type TaskOptions struct {
	GitInit     bool   `json:"gitInit" form:"gitInit" binding:"required"`
	GoInit      bool   `json:"goInit" form:"goInit" binding:"required"`
	SshKeyCopy  bool   `json:"sshKeyCopy" form:"sshKeyCopy" binding:"required"`
	Repo        string `json:"repo" form:"repo" binding:"required"`
	Branch      string `json:"branch" form:"branch" binding:"required"`
	AutoTrigger bool   `json:"autoTrigger" form:"autoTrigger"`
}

type Status string

const (
	Waiting Status = "waiting"
	Running Status = "running"
	Success Status = "success"
	Failed  Status = "failed"
	Timeout Status = "timeout"
	Cancel  Status = "cancel"
)

type RollbackStatus string

const (
	UnReady RollbackStatus = ""
	Ready   RollbackStatus = "ready"
	Cleaned RollbackStatus = "cleaned"
)

type TaskRecord struct {
	Ctx     context.Context               `json:"-"`
	Storage cache.PageStorage[TaskRecord] `json:"-"`

	ID string `json:"id"`
	TaskOptions
	Commit    string          `json:"commit"`
	GitHash   string          `json:"gitHash"`
	CreateAt  time.Time       `json:"createAt"`
	Status    Status          `json:"status"`
	CreateBy  string          `json:"createBy"`
	BuildFile string          `json:"buildFile"`
	Log       []TaskRecordLog `json:"log"`
	StartAt   time.Time       `json:"startAt"`
	FinishAt  time.Time       `json:"finishAt"`
	RBStatus  RollbackStatus  `json:"rbStatus"`
	RB        bool            `json:"rb"`
	RID       string          `json:"rid"`
}

type TaskRecordLog struct {
	Time  time.Time          `json:"time"`
	Text  string             `json:"text"`
	Level TaskRecordLogLevel `json:"level"`
}

type TaskRecordLogLevel string

const (
	Info  TaskRecordLogLevel = "info"
	Warn  TaskRecordLogLevel = "warn"
	Error TaskRecordLogLevel = "error"
	Light TaskRecordLogLevel = "light"
)

func (t *TaskRecord) Running(log string, level ...TaskRecordLogLevel) {
	var logLevel TaskRecordLogLevel
	if len(level) > 0 {
		logLevel = level[0]
	} else {
		logLevel = Info
	}
	t.Log = append(t.Log, TaskRecordLog{
		Time:  time.Now(),
		Text:  log,
		Level: logLevel,
	})
	if t.Storage == nil || t.ID == "" {
		return
	}

	get, err := t.Storage.Get(map[string]any{"id": t.ID})
	if err != nil {
		logger.Error(t.Ctx, fmt.Sprintf("Error getting task item: %v", err))
		return
	}
	if get == nil {
		logger.Error(t.Ctx, "Task item not found")
		return
	}
	if get.Status == Cancel || get.Status == Timeout || get.Status == Failed {
		logger.Error(t.Ctx, fmt.Sprintf("Task item already finished: %s", get.Status))
		t.Status = get.Status
		return
	}

	if logLevel == Error {
		t.Status = Failed
		t.FinishAt = time.Now()
	} else if t.Status == Waiting {
		t.Status = Running
		t.StartAt = time.Now()
	}

	if err := t.Storage.Update(map[string]any{"id": t.ID}, t); err != nil {
		logger.Error(t.Ctx, fmt.Sprintf("Error updating task item: %v", err))
	}
}

func (t *TaskRecord) TimeOut(log string) {
	t.Running(fmt.Sprintf(log), Error)
	t.Status = Timeout
	t.FinishAt = time.Now()
	storage := cache.NewPageStorage[TaskRecord](t.Ctx, cache.Sqlite)
	if err := storage.Update(map[string]any{"id": t.ID}, t); err != nil {
		logger.Error(t.Ctx, fmt.Sprintf("Error updating task item: %v", err))
	}
}
