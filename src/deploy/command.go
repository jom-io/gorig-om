package deploy

import (
	"bytes"
	"context"
	"fmt"
	"github.com/jom-io/gorig/utils/errors"
	"github.com/jom-io/gorig/utils/logger"
	"os"
	"os/exec"
	"strings"
)

func RunCommandLog(ctx context.Context, cmd string, args ...string) (string, *errors.Error) {
	return runCommand(ctx, true, "", nil, cmd, args...)
}

func RunCommand(ctx context.Context, cmd string, args ...string) (string, *errors.Error) {
	return runCommand(ctx, false, "", nil, cmd, args...)
}

func RunCommandEnv(ctx context.Context, env []string, cmd string, args ...string) (string, *errors.Error) {
	return runCommand(ctx, false, "", env, cmd, args...)
}

func RunCommandDir(ctx context.Context, dir string, cmd string, args ...string) (string, *errors.Error) {
	return runCommand(ctx, false, dir, nil, cmd, args...)
}

func runCommand(ctx context.Context, print bool, dir string, env []string, cmd string, args ...string) (string, *errors.Error) {
	if print {
		logger.Info(ctx, fmt.Sprintf("Running command: %s %s", cmd, strings.Join(args, " ")))
	}
	command := exec.Command(cmd, args...)
	var out bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &out
	command.Stderr = &stderr
	if dir != "" {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			return "", errors.Verify("Directory does not exist", err)
		}
		command.Dir = dir
	}
	if env != nil {
		command.Env = append(os.Environ(), env...)
	}

	err := command.Run()
	if err != nil {
		if !print {
			logger.Info(ctx, fmt.Sprintf("Running command: %s %s", cmd, strings.Join(args, " ")))
		}
		errInfo := fmt.Sprintf("Command failed: %s\n%s", err.Error(), stderr.String())
		logger.Error(ctx, errInfo)
		if stderr.Len() > 0 {
			return "", errors.Verify(errInfo)
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
	if print {
		logger.Info(ctx, fmt.Sprintf("Command output: %s", result))
	}

	return result, nil
}
