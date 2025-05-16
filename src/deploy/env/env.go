package deploy

import (
	"context"
	"fmt"
	"github.com/jom-io/gorig-om/src/deploy"
	"github.com/jom-io/gorig/cache"
	"github.com/jom-io/gorig/utils/errors"
	"github.com/jom-io/gorig/utils/logger"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

var Env envService

type envService struct {
}

func init() {
	Env = envService{}
}

const (
	GitRepoKey = "git_repo"
	BranchKey  = "deploy_branch"
	GOVersion  = "1.23.4"
)

// CheckGit checks if the git command is available
// and returns an error if it is not.
func (c envService) CheckGit(ctx context.Context) EnvVersion {
	logger.Info(ctx, "Checking if git is available...")
	gitVersion := EnvVersion{
		Installed: true,
	}
	result, err := deploy.RunCommand(ctx, "git", nil, "--version")
	if err != nil {
		gitVersion.Error = fmt.Sprintf("Git check failed, result:%v,err:%v", result, err)
		logger.Warn(ctx, gitVersion.Error)
		gitVersion.Installed = false

	}
	gitVersion.Version = result

	return gitVersion
}

// InstallGit installs git if it is not already installed
func (c envService) InstallGit(ctx context.Context) EnvVersion {
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

func (c envService) detectPackageManager() string {
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

func (c envService) installGit(ctx context.Context, manager string) (string, error) {

	switch manager {
	case "apt":
		//cmd = exec.Command("bash", "-c", "apt update && apt install -y git")
		return deploy.RunCommand(ctx, "bash", deploy.DefOpts(), "-c", "apt update && apt install -y git")
	case "yum":
		//cmd = exec.Command("bash", "-c", "yum install -y git")
		return deploy.RunCommand(ctx, "bash", deploy.DefOpts(), "-c", "yum install -y git")
	case "apk":
		//cmd = exec.Command("bash", "-c", "apk add git")
		return deploy.RunCommand(ctx, "bash", deploy.DefOpts(), "-c", "apk add git")
	default:
		return "", errors.Verify("Unsupported package manager")
	}
}

// Branches lists all branches in the git repository
func (c envService) Branches(ctx context.Context, repoURL string) ([]string, *errors.Error) {
	logger.Info(ctx, fmt.Sprintf("Listing branches for repository: %s", repoURL))

	// git", "ls-remote", "--heads", repoURL
	branches, errR := deploy.RunCommand(ctx, "git", nil, "ls-remote", "--heads", repoURL)
	if errR != nil {
		if strings.Contains(errR.Error(), "Host key verification failed") {
			logger.Warn(ctx, "Host key verification failed, trying to trust host...")
			host := c.extractGitHost(repoURL)
			if host != "" {
				if err := c.trustHost(ctx, host); err != nil {
					return nil, errors.Verify("Failed to trust host", err)
				}
				branches, errR = deploy.RunCommand(ctx, "git", nil, "ls-remote", "--heads", repoURL)
			}
		}
		if errR != nil {
			return nil, errors.Verify("Failed to list branches", errR)
		}
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

func (c envService) trustHost(ctx context.Context, host string) error {
	cmd := exec.CommandContext(ctx, "sh", "-c", fmt.Sprintf("ssh-keyscan %s >> ~/.ssh/known_hosts", host))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ssh-keyscan error: %v\noutput: %s", err, out)
	}
	return nil
}

func (c envService) extractGitHost(repoURL string) string {
	if strings.HasPrefix(repoURL, "git@") {
		// git@github.com:hootuu/ninepay.git
		if parts := strings.Split(repoURL, "@"); len(parts) == 2 {
			if hostPath := strings.SplitN(parts[1], ":", 2); len(hostPath) == 2 {
				return hostPath[0]
			}
		}
	}
	if u, err := url.Parse(repoURL); err == nil {
		return u.Hostname()
	}
	return ""
}

// GetSSHKey retrieves the SSH key for the git repository
func (c envService) GetSSHKey(ctx context.Context) SshKey {
	logger.Info(ctx, "Retrieving SSH key for git repository...")
	sshKey := SshKey{}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		sshKey.Error = fmt.Sprintf("Failed to get user home directory, err:%v", err)
		return sshKey
	}

	sshPath := filepath.Join(homeDir, ".ssh", "id_rsa.pub")

	if _, errExist := os.Stat(sshPath); !os.IsNotExist(errExist) {
		if result, errR := deploy.RunCommand(ctx, "cat", nil, sshPath); errR != nil {
			sshKey.Error = fmt.Sprintf("Failed to read SSH key, err:%v", errR)
		} else {
			sshKey.PublicKey = result
		}
	}

	return sshKey
}

// GenSSHKey generates a new SSH key for the git repository
func (c envService) GenSSHKey(ctx context.Context) SshKey {
	logger.Info(ctx, "Generating new SSH key for git repository...")
	sshKey := SshKey{}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		sshKey.Error = fmt.Sprintf("Failed to get user home directory, err:%v", err)
		return sshKey
	}

	sshPath := filepath.Join(homeDir, ".ssh", "id_rsa")

	if _, errExist := os.Stat(sshPath); os.IsNotExist(errExist) {
		hostname, errH := deploy.RunCommand(ctx, "hostname", nil)
		if errH != nil {
			logger.Warn(ctx, fmt.Sprintf("Failed to retrieve hostname, err:%v", errH))
		}
		if hostname == "" {
			hostname = fmt.Sprintf("gen_%d", time.Now().Unix())
		}
		hostname = fmt.Sprintf("%s@%s", "gorig", hostname)

		if _, errR := deploy.RunCommand(ctx, "ssh-keygen", deploy.DefOpts(), "-t", "rsa", "-b", "4096", "-f", sshPath, "-C", hostname, "-N", ""); errR != nil {
			sshKey.Error = fmt.Sprintf("Failed to generate SSH key, err:%v", errR)
			return sshKey
		}
	}
	return c.GetSSHKey(ctx)
}

func (c envService) GetLatestHash(ctx context.Context, repo, branch string) string {
	//logger.Info(ctx, "Retrieving latest git hash...")

	if repo == "" || branch == "" {
		logger.Warn(ctx, "Repository URL or branch is empty")
		return ""
	}
	//  git ls-remote git@github.com-jom:jom-io/gorig.git refs/heads/master
	hash, err := deploy.RunCommand(ctx, "git", nil, "ls-remote", "--heads", repo, branch)
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

func (c envService) CheckGo(ctx context.Context) EnvVersion {
	logger.Info(ctx, "Checking if go is available...")
	goVersion := EnvVersion{
		Installed: true,
	}
	result, err := deploy.RunCommand(ctx, "go", deploy.DefOpts(), "version")
	if err != nil {
		goVersion.Error = fmt.Sprintf("Go check, result:%v,err:%v", result, err)
		logger.Warn(ctx, goVersion.Error)
		goVersion.Installed = false
	}

	if strings.Contains(result, "go version") {
		newResult := strings.TrimPrefix(result, "go version go")
		newResult = strings.Split(newResult, " ")[0]
		if versionCompare(newResult, GOVersion) < 0 {
			goVersion.Error = fmt.Sprintf("Go version is lower than %s, current version: %s", GOVersion, newResult)
			goVersion.Installed = false
			logger.Warn(ctx, goVersion.Error)
		}
	} else {
		if goVersion.Error == "" {
			goVersion.Error = fmt.Sprintf("Go version not found %s", result)
		}
		logger.Warn(ctx, goVersion.Error)
		goVersion.Installed = false
		return goVersion
	}

	goVersion.Version = result
	return goVersion
}

func (c envService) InitGo(ctx context.Context) EnvVersion {
	logger.Info(ctx, "Installing go...")
	goVersion := c.CheckGo(ctx)
	if goVersion.Installed {
		logger.Warn(ctx, "Go is already installed")
		return goVersion
	}

	if _, err := c.installGo(ctx); err != nil {
		goVersion.Error = fmt.Sprintf("Failed to install go: %v", err)
		logger.Warn(ctx, goVersion.Error)
		return goVersion
	}

	return c.CheckGo(ctx)
}

func (c envService) installGo(ctx context.Context) (EnvVersion, *errors.Error) {

	cmd := fmt.Sprintf(`
set -e
wget https://dl.google.com/go/go%s.linux-amd64.tar.gz
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf go%s.linux-amd64.tar.gz
echo 'export PATH=$PATH:/usr/local/go/bin' | sudo tee /etc/profile.d/go.sh > /dev/null
sudo chmod +x /etc/profile.d/go.sh
source /etc/profile.d/go.sh
sudo rm -rf go%s.linux-amd64.*
go version
`, GOVersion, GOVersion, GOVersion)

	output, err := deploy.RunCommand(ctx, "bash", deploy.DefOpts(), "-c", cmd)
	if err != nil {
		return EnvVersion{}, errors.Verify(fmt.Sprintf("output:%s \n err:%v", output, err))
	}

	return c.CheckGo(ctx), nil
}

// versionCompare compares two version strings and returns: 1 if version1 > version2, -1 if version1 < version2, and 0 if they are equal.
func versionCompare(version1, version2 string) int {
	v1 := strings.Split(version1, ".")
	v2 := strings.Split(version2, ".")

	for i := 0; i < len(v1) || i < len(v2); i++ {
		var num1, num2 int
		if i < len(v1) {
			num1, _ = strconv.Atoi(v1[i])
		}
		if i < len(v2) {
			num2, _ = strconv.Atoi(v2[i])
		}

		if num1 < num2 {
			return -1
		} else if num1 > num2 {
			return 1
		}
	}

	return 0
}

var EnvDefault = []GoEnv{
	{"GOARCH", "amd64", true},
	{"GOOS", "linux", true},
	{"CGO_ENABLED", "0", false},
	{"GO111MODULE", "on", false},
}

func (c envService) GoEnvSet(ctx context.Context, env []GoEnv) *errors.Error {
	logger.Info(ctx, fmt.Sprintf("Setting go env %v", env))
	for _, e := range EnvDefault {
		found := false
		for _, e2 := range env {
			if e2.Key == e.Key {
				found = true
				break
			}
		}
		if !found {
			env = append([]GoEnv{e}, env...)
		}
	}

	var deleteEnvs []string
	getEnv, err := cache.New[[]GoEnv](cache.JSON).Get("go_env")
	if err != nil {
		logger.Error(ctx, fmt.Sprintf("Failed to get go env %v", err))
	}
	if getEnv != nil {
		for _, e := range getEnv {
			found := false
			for _, e2 := range env {
				if e2.Key == e.Key {
					found = true
					break
				}
			}
			if !found && !e.Default {
				deleteEnvs = append(deleteEnvs, e.Key)
			}
		}
	}

	for _, e := range deleteEnvs {
		if err := os.Unsetenv(e); err != nil {
			logger.Error(ctx, fmt.Sprintf("Failed to unset env %s %v", e, err))
		}
		if _, err := deploy.RunCommand(ctx, "go", deploy.DefOpts(), "env", "-u", e); err != nil {
			logger.Error(ctx, fmt.Sprintf("Failed to unset go env %s %v", e, err))
		}
	}

	if err := cache.New[[]GoEnv](cache.JSON).Set("go_env", env, 0); err != nil {
		return errors.Verify(fmt.Sprintf("Failed to set go env %v", err))
	}
	return nil
}

func (c envService) GoEnvGet(ctx context.Context) []GoEnv {
	logger.Info(ctx, fmt.Sprintf("Getting go env"))
	env, err := cache.New[[]GoEnv](cache.JSON).Get("go_env")
	if err != nil {
		logger.Error(ctx, fmt.Sprintf("Failed to get go env %v", err))
		return nil
	}
	if env == nil {
		env = []GoEnv{}
		env = append(env, EnvDefault...)
		if e := c.GoEnvSet(ctx, env); e != nil {
			logger.Error(ctx, fmt.Sprintf("Goenv init failed to set go env %v", e))
			return nil
		}
		return nil
	}
	logger.Info(ctx, fmt.Sprintf("Got go env %v", env))
	return env
}
