package app

import (
	"context"
	"fmt"
	"github.com/jom-io/gorig-om/src/deploy"
	"github.com/jom-io/gorig/cache"
	"github.com/jom-io/gorig/global/variable"
	"github.com/jom-io/gorig/mid/messagex"
	configure "github.com/jom-io/gorig/utils/cofigure"
	"github.com/jom-io/gorig/utils/errors"
	"github.com/jom-io/gorig/utils/logger"
	"github.com/jom-io/gorig/utils/sys"
	"github.com/rs/xid"
	"os"
	"strings"
)

var App appService

type appService struct {
}

const startIDKey = "startID"

var watchdogFile string

func init() {
	App = appService{}
	watchdogFile = fmt.Sprintf("%s_%s.sh", "watchdog", sys.RunMode)
}

type RunBack func(log string)

func getRunFileName() (string, *errors.Error) {
	runFile := fmt.Sprintf("%s-%s.linux64", variable.SysName, sys.RunMode)
	runFile = strings.ToLower(runFile)
	runFile = strings.ReplaceAll(runFile, "_", "-")
	if _, err := os.Stat(runFile); os.IsNotExist(err) {
		return "", errors.Verify("Run file not found", err)
	}
	return runFile, nil
}

func (a appService) Restart(ctx context.Context, runFile string, runBack RunBack, itemIDs ...string) *errors.Error {
	logger.Info(ctx, "Restarting application...")
	if runFile == "" {
		if getFile, e := getRunFileName(); e != nil {
			return e
		} else {
			runFile = getFile
		}
	}
	startID := xid.New().String()
	if err := cache.New[string](cache.JSON).Set(startIDKey, startID, 0); err != nil {
		return errors.Verify("Failed to set startID", err)
	}
	getStartID, err := cache.New[string](cache.JSON).Get(startIDKey)
	if err != nil {
		return errors.Verify("Failed to get startID", err)
	}
	logger.Info(ctx, fmt.Sprintf("Start ID: %s", getStartID))

	if _, err := os.Stat(runFile); os.IsNotExist(err) {
		return errors.Verify("Restart run file not found", err)
	}

	fileInfo, err := os.Stat(runFile)
	if err != nil {
		return errors.Verify("Restart failed to get file info", err)
	}
	if fileInfo.Mode()&0111 == 0 {
		if err := os.Chmod(runFile, 0755); err != nil {
			return errors.Verify("Failed to change file permissions", err)
		}
		logger.Info(nil, "Restart file permissions changed to executable")
	}

	runBack("Service restart ...")

	restartFile := "restart.sh"

	runBack(fmt.Sprintf("Start ID: %s", getStartID))
	port := configure.GetString("api.rest.addr", ":9617")
	var itemID string
	if len(itemIDs) > 0 {
		itemID = itemIDs[0]
	}
	reStartAddr := fmt.Sprintf("http://127.0.0.1%s/om/app/restarted?startID=%s&itemID=%s", port, startID, itemID)

	content := fmt.Sprintf(`#!/bin/bash
	echo "Service restarting..."
	echo "Stopping service..."
	pkill -15 -f %s
	timeout=0
	while pgrep -f %s.linux64 > /dev/null; do
	   echo "Waiting for the service to stop..."
	   timeout=$(($timeout+1))
	   if [ $timeout -gt 10 ]; then
	       echo "Service stop failed. Force stop."
	       pkill -9 -f %s
	       break
	   fi
	   sleep 1
	done
	echo "Service stopped successfully."
	echo "Starting service..."
	export GORIG_SYS_MODE=%s
	nohup ./%s > nohup.out 2>&1 &
	pid=$!
	echo "Service started with PID: $pid"
	echo "Checking service startup status..."
	sleep 2
	check_timeout=0
	url="%s"
	while true; do
	   http_code=$(curl -s -o /dev/null -w "%%{http_code}" "$url")
	   echo "Check $url => HTTP $http_code"
	   if [ "$http_code" = "200" ]; then
	       echo "Service restarted."
	       break
	   fi
	   check_timeout=$(($check_timeout+1))
	   if [ $check_timeout -ge 120 ]; then
	       echo "Service start failed: Timeout reached."
	       exit 1
	   fi
	   sleep 1
	done
	`, runFile, runFile, runFile, sys.RunMode, runFile, reStartAddr)

	if errW := os.WriteFile(restartFile, []byte(content), 0755); errW != nil {
		return errors.Verify("Failed to write to restart.sh file", errW)
	}

	content = fmt.Sprintf(`#!/bin/bash
	echo "Watchdog service started at: $(date)"
	while true; do
	   echo "Checking at: $(date)"
	   if ! pgrep -f %s > /dev/null; then
	       echo "Service is not running. Restarting..."
	       mkdir -p restart_logs
	       cp nohup.out restart_logs/auto_restart_$(date +%%Y%%m%%d%%H%%M%%S).log
	       ./restart.sh
	   fi
	   sleep 5
	done`, runFile)

	if errW := os.WriteFile(watchdogFile, []byte(content), 0755); errW != nil {
		return errors.Verify("Failed to write to watchdog file", errW)
	}

	if _, rErr := deploy.RunCommand(ctx, "echo", nil, "Stopping watchdog service..."); rErr != nil {
		return rErr
	} else {
		runBack("Stopping watchdog service...")
	}
	if _, rErr := deploy.RunCommand(ctx, "pkill", nil, "-9", "-f", fmt.Sprintf("watchdog_%s.sh", sys.RunMode)); rErr != nil {
		return rErr
	} else {
		runBack("Watchdog service stopped.")
	}

	runBack("restarting service...")
	if _, rErr := deploy.RunCommand(ctx, "bash", nil, "-c", "nohup ./restart.sh > restart.log 2>&1 &"); rErr != nil {
		runBack(fmt.Sprintf("Failed to execute restart.sh in background: %v", rErr))
	}

	return nil
}

func (a appService) RestartSuccess(ctx context.Context, id, itemID string) {
	logger.Info(ctx, "Restarting application...")
	localID, err := cache.New[string](cache.JSON).Get(startIDKey)
	if err != nil {
		logger.Error(ctx, "Failed to get local startID")
		return
	}

	if localID != "" {
		if localID != id {
			logger.Error(ctx, fmt.Sprintf("Local startID %s does not match request startID %s", localID, id))
			return
		}
		go func() {
			if err := cache.New[string](cache.JSON).Del(startIDKey); err != nil {
				logger.Error(ctx, "Failed to delete local startID")
				return
			}
		}()

		if itemID != "" {
			go func() {
				messagex.PublishNewMsg(ctx, deploy.TopicRunStarted, map[string]string{
					"itemID": itemID,
				})
			}()
		}

		go func() {
			if _, rErr := deploy.RunCommand(ctx, "echo", nil, "Starting watchdog service..."); rErr != nil {
				logger.Error(ctx, "Failed to start watchdog service")
				return
			}

			if _, rErr := deploy.RunCommand(ctx, "bash", nil, "-c", fmt.Sprintf("nohup ./%s > watchdog.out 2>&1 &", watchdogFile)); rErr != nil {
				logger.Error(ctx, "Failed to start watchdog service")
				return
			} else {
				logger.Info(ctx, "Watchdog service started.")
			}
		}()
	}

	//	time.Sleep(3 * time.Second)
	//	var runErr *errors.Error
	//	if r, rErr := deploy.RunCommand(ctx, "bash", nil, "-c", fmt.Sprintf("ps -ef | grep %s | grep -v grep", runFile)); rErr != nil {
	//		return rErr
	//	} else {
	//		if len(r) == 0 {
	//			logger.Error(ctx, "Service is not running")
	//			runErr = errors.Verify("Service is not running")
	//			goto end
	//		}
	//	}
	//
	//	if _, rErr := deploy.RunCommand(ctx, "echo", nil, "Starting watchdog service..."); rErr != nil {
	//		return rErr
	//	} else {
	//		runBack("Starting watchdog service...")
	//	}
	//	if _, rErr := deploy.RunCommand(ctx, "bash", nil, "-c", fmt.Sprintf("nohup ./%s > watchdog.out 2>&1 &", watchdogFile)); rErr != nil {
	//		return rErr
	//	} else {
	//		runBack("Watchdog service started.")
	//	}
	//
	//end:
	//	if log, rErr := deploy.RunCommand(ctx, "cat", nil, "nohup.out"); rErr != nil {
	//		return rErr
	//	} else {
	//		runBack(log)
	//	}
	//
	//	if runErr != nil {
	//		return runErr
	//	}

}

func (a appService) Stop(ctx context.Context) *errors.Error {
	logger.Info(ctx, "Stopping application...")

	stopFile := "stop.sh"
	//if _, err := os.Stat(stopFile); os.IsNotExist(err) {
	//file, errC := os.Create(stopFile)
	//if errC != nil {
	//	return "", errors.Verify("Failed to create stop.sh file", errC)
	//}
	//defer file.Close()

	runFile, _ := getRunFileName()

	content := fmt.Sprintf(`#!/bin/bash
echo "Stopping watchdog service..."
pkill -9 -f watchdog_%s.sh
echo "Stopping service..."
pkill -15 -f %s
echo "Service stopped successfully."`, sys.RunMode, runFile)

	if errW := os.WriteFile(stopFile, []byte(content), 0755); errW != nil {
		return errors.Verify("Failed to write to stop.sh file", errW)
	}

	go func() {
		if _, err := deploy.RunCommand(ctx, "./stop.sh", deploy.DefOpts()); err != nil {
			logger.Error(ctx, fmt.Sprintf("Failed to execute stop.sh: %v", err))
			return
		}
	}()

	return nil
}

func (a appService) Clean(ctx context.Context) *errors.Error {
	logger.Info(ctx, "Cleaning files...")

	files := []string{"restart.sh", "stop.sh", fmt.Sprintf("watchdog_%s.sh", sys.RunMode), "nohup.out", "restart_logs", "watchdog.out"}
	for _, file := range files {
		if _, err := os.Stat(file); os.IsNotExist(err) {
			continue
		}

		if err := os.RemoveAll(file); err != nil {
		}
		logger.Info(ctx, fmt.Sprintf("Removed file: %s", file))
	}

	return nil
}
