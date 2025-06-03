package app

import (
	"context"
	"github.com/jom-io/gorig/cache"
	"github.com/jom-io/gorig/utils/errors"
	"time"
)

type StartSrc string

const (
	StartSrcManual StartSrc = "manual"
	StartSrcDeploy StartSrc = "deploy"
	StartSrcCrash  StartSrc = "crash"
)

func (s StartSrc) String() string {
	return string(s)
}

type ReStartLog struct {
	StartTime int64    `json:"startTime"`
	StartSrc  StartSrc `json:"startSrc"`
	Log       string   `json:"log"`
}

func (re *ReStartLog) Save(ctx context.Context, startSrc StartSrc, log string) *errors.Error {
	re.StartSrc = startSrc
	re.StartTime = time.Now().Unix()
	re.Log = log
	if err := cache.NewPager[ReStartLog](ctx, cache.Sqlite).Put(*re); err != nil {
		return errors.Verify("Failed to save restart log", err)
	}
	return nil
}

func ReStartPage(ctx context.Context, page, size int64) (*cache.PageCache[ReStartLog], *errors.Error) {
	items, err := cache.NewPager[ReStartLog](ctx, cache.Sqlite).Find(page, size, nil, cache.PageSorterDesc("startTime"))
	if err != nil {
		return nil, errors.Verify("Failed to get last start log", err)
	}
	return items, nil
}
