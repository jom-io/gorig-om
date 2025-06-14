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
	"io"
	"os"
	"strings"
	"time"
)

var App appService

type appService struct {
}

const startIDKey = "startID"

var watchdogFile string

func init() {
	App = appService{}
	watchdogFile = fmt.Sprintf("%s_%s_%s.sh", "watchdog", variable.SysName, sys.RunMode)
	watchdogFile = strings.ToLower(watchdogFile)
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

	src := StartSrcManual
	if runBack == nil {
		runBack = func(log string) {
			logger.Info(ctx, log)
		}
	} else {
		src = StartSrcDeploy
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
    echo "$(date)"
	echo "Service restarting..."
	if [ -z "$1" ]; then
	   src="%s"
	else
	   src="$1"
	fi
    echo "Restart source: $src"
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
	url="%s&pid=$pid&src=$src"
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
	`, StartSrcManual, runFile, runFile, runFile, sys.RunMode, runFile, reStartAddr)

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
	       ./restart.sh %s
	   fi
	   sleep 5
	done`, runFile, StartSrcCrash.String())

	if errW := os.WriteFile(watchdogFile, []byte(content), 0755); errW != nil {
		return errors.Verify("Failed to write to watchdog file", errW)
	}

	if _, rErr := deploy.RunCommand(ctx, "echo", nil, "Stopping watchdog service..."); rErr != nil {
		return rErr
	} else {
		runBack("Stopping watchdog service...")
	}
	if _, rErr := deploy.RunCommand(ctx, "pkill", nil, "-9", "-f", watchdogFile); rErr != nil {
		return rErr
	} else {
		runBack("Watchdog service stopped.")
	}

	runBack("Restarting service...")
	if _, rErr := deploy.RunCommand(ctx, "bash", deploy.DefOpts().SetNice(5), "-c", fmt.Sprintf("nohup ./restart.sh %s > restart.log 2>&1 &", src.String())); rErr != nil {
		runBack(fmt.Sprintf("Failed to execute restart.sh in background: %v", rErr))
	}

	return nil
}

func (a appService) RestartSuccess(ctx context.Context, startID, itemID, pid string, src StartSrc) {
	logger.Info(ctx, "Restarting application...")
	c := cache.New[string](cache.JSON)
	localID, err := c.Get(startIDKey)
	if err != nil {
		logger.Error(ctx, "Failed to get local startID")
		return
	}

	go func() {
		reStartLog := &ReStartLog{}
		reStartLogFile := "restart.log"
		log := ""
		time.Sleep(1 * time.Second) // Ensure the restart log file is created before reading
		if _, errOs := os.Stat(reStartLogFile); os.IsNotExist(errOs) {
			logger.Error(ctx, "Restart log file not found")
			log = fmt.Sprintf("Restarted at %s with ID %s", time.Now().Format("2006-01-02 15:04:05"), startID)
		} else {
			logContent, errRead := os.ReadFile(reStartLogFile)
			if errRead != nil {
				logger.Error(ctx, "Failed to read restart log file")
				return
			}
			log = string(logContent)
		}
		if src == StartSrcCrash {
			restartLogs, err := os.ReadDir("restart_logs")
			if err != nil {
				logger.Error(ctx, "Failed to read restart_logs directory")
				return
			}
			if len(restartLogs) > 0 {
				latestLog := restartLogs[0]
				for _, logFile := range restartLogs {
					if logFile.IsDir() {
						continue
					}
					if logFile.Name() > latestLog.Name() {
						latestLog = logFile
					}
				}
				logger.Info(ctx, fmt.Sprintf("Latest restart log file: %s", latestLog.Name()))
				crashLog, errLog := readLastNLines("restart_logs/"+latestLog.Name(), 300)
				if errLog != nil {
					logger.Error(ctx, "Failed to read last lines of latest restart log file")
					return
				}
				log = fmt.Sprintf("%s\nCrash log:\n%s", log, crashLog)
			} else {
				log = fmt.Sprintf("%s\nNo crash logs found.", log)
			}
		}
		if errStart := reStartLog.Save(ctx, src, log); errStart != nil {
			logger.Error(ctx, "Failed to save restart log")
			return
		}
	}()

	if localID != "" {
		if localID != startID {
			logger.Error(ctx, fmt.Sprintf("Local startID %s does not match request startID %s", localID, startID))
			return
		}
		go func() {
			if err := cache.New[string](cache.JSON).Del(startIDKey); err != nil {
				logger.Error(ctx, "Failed to delete local startID")
				return
			}
		}()

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

			if itemID != "" {
				go func() {
					messagex.PublishNewMsg(ctx, deploy.TopicRunStarted, map[string]string{
						"itemID": itemID,
						"pid":    pid,
					})
				}()
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

func readLastNLines(path string, n int) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	var (
		lines       []string
		buffer      []byte
		chunkSize   int64 = 4096
		fileInfo, _       = f.Stat()
		fileSize          = fileInfo.Size()
		offset            = fileSize
		leftover    []byte
	)

	for offset > 0 && len(lines) <= n {
		if offset < chunkSize {
			chunkSize = offset
		}
		offset -= chunkSize
		buf := make([]byte, chunkSize)

		_, err := f.ReadAt(buf, offset)
		if err != nil && err != io.EOF {
			return "", err
		}

		buffer = append(buf, leftover...)
		parts := strings.Split(string(buffer), "\n")
		if len(parts) > 0 {
			leftover = []byte(parts[0])
			parts = parts[1:]
		}

		lines = append(parts, lines...)
	}

	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}

	return strings.Join(lines, "\n"), nil
}
