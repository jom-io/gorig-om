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

func RunCommand(ctx context.Context, cmd string, args ...string) (string, *errors.Error) {
	return runCommand(ctx, "", cmd, args...)
}

func RunCommandDir(ctx context.Context, dir string, cmd string, args ...string) (string, *errors.Error) {
	return runCommand(ctx, dir, cmd, args...)
}

func runCommand(ctx context.Context, dir string, cmd string, args ...string) (string, *errors.Error) {
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

	err := command.Run()
	if err != nil {
		logger.Info(ctx, fmt.Sprintf("Running command: %s %s", cmd, args))
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

	return result, nil
}
