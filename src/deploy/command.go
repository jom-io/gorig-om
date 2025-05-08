package deploy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/jom-io/gorig/global/consts"
	"github.com/jom-io/gorig/mid/messagex"
	localErrs "github.com/jom-io/gorig/utils/errors"
	"github.com/jom-io/gorig/utils/logger"
	"github.com/spf13/cast"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	runTimeoutDef   = 1 * time.Minute
	TopicRunTimeout = "run_timeout"
)

type RunOpts struct {
	Dir      string
	Env      []string
	PrintLog bool
	TimeOut  time.Duration
}

func DefOpts() *RunOpts {
	return &RunOpts{
		Dir:      "",
		Env:      nil,
		PrintLog: true,
		TimeOut:  runTimeoutDef,
	}
}

func (opts *RunOpts) SetDir(dir string) *RunOpts {
	opts.Dir = dir
	return opts
}

func (opts *RunOpts) SetEnv(env []string) *RunOpts {
	opts.Env = env
	return opts
}

func (opts *RunOpts) SetPrintLog(printLog bool) *RunOpts {
	opts.PrintLog = printLog
	return opts
}

func (opts *RunOpts) SetTimeOut(timeOut time.Duration) *RunOpts {
	opts.TimeOut = timeOut
	return opts
}

func (opts *RunOpts) DirExists() bool {
	if opts.Dir == "" {
		return false
	}
	if _, err := os.Stat(opts.Dir); os.IsNotExist(err) {
		return false
	}
	return true
}

func (opts *RunOpts) EnvExists() bool {
	if opts.Env == nil {
		return true
	}
	for _, env := range opts.Env {
		if env == "" {
			return false
		}
	}
	return true
}

func (opts *RunOpts) PrintLogEnabled() bool {
	if opts.PrintLog {
		return true
	}
	return false
}

func (opts *RunOpts) TimeOutValid() bool {
	if opts.TimeOut > 0 {
		return true
	}
	return false
}

func RunCommand(ctx context.Context, cmd string, runOpts *RunOpts, args ...string) (string, *localErrs.Error) {
	if runOpts == nil {
		runOpts = &RunOpts{
			TimeOut: runTimeoutDef,
		}
	}
	if runOpts.TimeOutValid() {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, runOpts.TimeOut)
		defer cancel()
		defer func() {
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				_ = messagex.Publish(fmt.Sprintf("%s.%s", TopicRunTimeout, cast.ToString(ctx.Value(consts.TraceIDKey))), nil)
			}
		}()
	}

	return runCommand(ctx, cmd, runOpts, args...)
}

func runCommand(ctx context.Context, cmd string, opts *RunOpts, args ...string) (string, *localErrs.Error) {
	if opts.PrintLogEnabled() {
		logger.Info(ctx, fmt.Sprintf("Running command: %s %s", cmd, strings.Join(args, " ")))
	}

	command := exec.CommandContext(ctx, cmd, args...)
	var out bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &out
	command.Stderr = &stderr
	if opts.DirExists() {
		command.Dir = opts.Dir
	}
	if opts.EnvExists() {
		command.Env = append(os.Environ(), opts.Env...)
	}

	err := command.Run()

	if err != nil {
		if !opts.PrintLogEnabled() {
			logger.Info(ctx, fmt.Sprintf("Running command: %s %s", cmd, strings.Join(args, " ")))
		}
		errInfo := fmt.Sprintf("Command failed: %s\n%s", err.Error(), stderr.String())
		logger.Error(ctx, errInfo)
		if stderr.Len() > 0 {
			return "", localErrs.Verify(errInfo)
		} else {
			return "", nil
		}
	}

	result := out.String()
	output := strings.Split(result, "\n")
	//for _, line := range output {
	//	if strings.TrimSpace(line) != "" {
	//		logger.Info(ctx, line)
	//	}
	//}

	if len(output) > 0 {
		result = strings.TrimSuffix(result, "\n")
	}
	if opts.PrintLogEnabled() {
		logger.Info(ctx, fmt.Sprintf("Command output: %s", result))
	}

	return result, nil
}
