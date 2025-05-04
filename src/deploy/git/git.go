package deploy

import (
	"context"
	"fmt"
	"github.com/jom-io/gorig-om/src/deploy"
	"github.com/jom-io/gorig/utils/errors"
	"github.com/jom-io/gorig/utils/logger"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

var Git gitService

type gitService struct {
}

func init() {
	Git = gitService{}
}

const (
	GitRepoKey = "git_repo"
	BranchKey  = "deploy_branch"
)

// CheckGit checks if the git command is available
// and returns an error if it is not.
func (c gitService) CheckGit(ctx context.Context) GitVersion {
	logger.Info(ctx, "Checking if git is available...")
	gitVersion := GitVersion{
		Installed: true,
	}
	result, err := deploy.RunCommand(ctx, "git", "--version")
	if err != nil {
		gitVersion.Error = fmt.Sprintf("Git check failed, result:%v,err:%v", result, err)
		logger.Warn(ctx, gitVersion.Error)
		gitVersion.Installed = false

	}
	gitVersion.Version = result

	return gitVersion
}

// InstallGit installs git if it is not already installed
func (c gitService) InstallGit(ctx context.Context) GitVersion {
	logger.Info(ctx, "Installing git...")

	gitVersion := c.CheckGit(ctx)
	if gitVersion.Installed {
		logger.Warn(ctx, "Git is already installed")
		return gitVersion
	}

	manager := c.detectPackageManager()
	if manager == "" {
		gitVersion.Error = "No package manager found,please install apt/yum/apk"
		logger.Warn(ctx, gitVersion.Error)
		return gitVersion
	}

	if _, err := c.installGit(ctx, manager); err != nil {
		gitVersion.Error = fmt.Sprintf("Failed to install git using %s: %v", manager, err)
		logger.Warn(ctx, gitVersion.Error)
		return gitVersion
	}

	return c.CheckGit(ctx)
}

func (c gitService) detectPackageManager() string {
	if _, err := exec.LookPath("apt"); err == nil {
		return "apt"
	}
	if _, err := exec.LookPath("yum"); err == nil {
		return "yum"
	}
	if _, err := exec.LookPath("apk"); err == nil {
		return "apk" // Alpine Linux
	}
	return ""
}

func (c gitService) installGit(ctx context.Context, manager string) (string, error) {

	switch manager {
	case "apt":
		//cmd = exec.Command("bash", "-c", "apt update && apt install -y git")
		return deploy.RunCommand(ctx, "bash", "-c", "apt update && apt install -y git")
	case "yum":
		//cmd = exec.Command("bash", "-c", "yum install -y git")
		return deploy.RunCommand(ctx, "bash", "-c", "yum install -y git")
	case "apk":
		//cmd = exec.Command("bash", "-c", "apk add git")
		return deploy.RunCommand(ctx, "bash", "-c", "apk add git")
	default:
		return "", errors.Verify("Unsupported package manager")
	}
}

// Branches lists all branches in the git repository
func (c gitService) Branches(ctx context.Context, repoURL string) ([]string, *errors.Error) {
	logger.Info(ctx, fmt.Sprintf("Listing branches for repository: %s", repoURL))

	// git", "ls-remote", "--heads", repoURL
	branches, errR := deploy.RunCommand(ctx, "git", "ls-remote", "--heads", repoURL)
	if errR != nil {
		return nil, errors.Verify("Failed to list branches", errR)
	}

	branchList := strings.Split(branches, "\n")
	var branchNames []string

	for _, branch := range branchList {
		if strings.Contains(branch, "refs/heads/") {
			if strings.Contains(branch, "\t") {
				branch = strings.Split(branch, "\t")[1]
			}
			branchName := strings.TrimPrefix(branch, "refs/heads/")
			branchNames = append(branchNames, branchName)
		}
	}

	logger.Info(ctx, fmt.Sprintf("Branches found: %v", branchNames))
	return branchNames, nil
}

// GetSSHKey retrieves the SSH key for the git repository
func (c gitService) GetSSHKey(ctx context.Context) SshKey {
	logger.Info(ctx, "Retrieving SSH key for git repository...")
	sshKey := SshKey{}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		sshKey.Error = fmt.Sprintf("Failed to get user home directory, err:%v", err)
		return sshKey
	}

	sshPath := filepath.Join(homeDir, ".ssh", "id_rsa.pub")

	if _, errExist := os.Stat(sshPath); !os.IsNotExist(errExist) {
		if result, errR := deploy.RunCommand(ctx, "cat", sshPath); errR != nil {
			sshKey.Error = fmt.Sprintf("Failed to read SSH key, err:%v", errR)
		} else {
			sshKey.PublicKey = result
		}
	}

	return sshKey
}

// GenSSHKey generates a new SSH key for the git repository
func (c gitService) GenSSHKey(ctx context.Context) SshKey {
	logger.Info(ctx, "Generating new SSH key for git repository...")
	sshKey := SshKey{}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		sshKey.Error = fmt.Sprintf("Failed to get user home directory, err:%v", err)
		return sshKey
	}

	sshPath := filepath.Join(homeDir, ".ssh", "id_rsa")

	if _, errExist := os.Stat(sshPath); os.IsNotExist(errExist) {
		hostname, errH := deploy.RunCommand(ctx, "hostname")
		if errH != nil {
			logger.Warn(ctx, fmt.Sprintf("Failed to retrieve hostname, err:%v", errH))
		}
		if hostname == "" {
			hostname = fmt.Sprintf("gen_%d", time.Now().Unix())
		}
		hostname = fmt.Sprintf("%s@%s", "gorig", hostname)

		if _, errR := deploy.RunCommand(ctx, "ssh-keygen", "-t", "rsa", "-b", "4096", "-f", sshPath, "-C", hostname, "-N", ""); errR != nil {
			sshKey.Error = fmt.Sprintf("Failed to generate SSH key, err:%v", errR)
			return sshKey
		}
	}
	return c.GetSSHKey(ctx)
}

func (c gitService) GetLatestHash(ctx context.Context, repo, branch string) string {
	//logger.Info(ctx, "Retrieving latest git hash...")

	if repo == "" || branch == "" {
		logger.Warn(ctx, "Repository URL or branch is empty")
		return ""
	}
	//  git ls-remote git@github.com-jom:jom-io/gorig.git refs/heads/master
	hash, err := deploy.RunCommand(ctx, "git", "ls-remote", "--heads", repo, branch)
	if err != nil {
		logger.Warn(ctx, fmt.Sprintf("Failed to retrieve latest git hash, err:%v", err))
		return ""
	}
	if strings.Contains(hash, "refs/heads") {
		hash = strings.Split(hash, "refs/heads")[0]
	}
	hash = strings.TrimSpace(hash)
	return hash
}
