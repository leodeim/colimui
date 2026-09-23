package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const releaseRepository = "leodeim/colimui"

const installScriptURL = "https://raw.githubusercontent.com/" + releaseRepository + "/main/scripts/install.sh"

var latestReleaseURL = "https://api.github.com/repos/" + releaseRepository + "/releases/latest"

func checkForUpdateCmd() tea.Cmd {
	return func() tea.Msg {
		if version == "dev" {
			return updateCheckMsg{}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		latest, err := latestRelease(ctx)
		if err != nil || !isNewerVersion(latest, version) {
			return updateCheckMsg{}
		}
		return updateCheckMsg{version: latest}
	}
}

func latestRelease(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestReleaseURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub returned %s", resp.Status)
	}
	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&release); err != nil {
		return "", err
	}
	if !validVersion(release.TagName) {
		return "", fmt.Errorf("invalid release tag %q", release.TagName)
	}
	return release.TagName, nil
}

func isNewerVersion(candidate, current string) bool {
	candidateParts, candidateOK := versionParts(candidate)
	currentParts, currentOK := versionParts(current)
	if !candidateOK || !currentOK {
		return false
	}
	for i := range candidateParts {
		if candidateParts[i] != currentParts[i] {
			return candidateParts[i] > currentParts[i]
		}
	}
	return false
}

func validVersion(value string) bool {
	_, ok := versionParts(value)
	return ok
}

func versionParts(value string) ([3]int, bool) {
	var parts [3]int
	value = strings.TrimPrefix(value, "v")
	values := strings.Split(value, ".")
	if len(values) != len(parts) {
		return parts, false
	}
	for i, value := range values {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 0 {
			return parts, false
		}
		parts[i] = parsed
	}
	return parts, true
}

func update() error {
	if version == "dev" {
		return errors.New("cannot update a development build")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	latest, err := latestRelease(ctx)
	if err != nil {
		return err
	}
	if !isNewerVersion(latest, version) {
		fmt.Printf("colimui %s is already up to date\n", version)
		return nil
	}

	binary, err := releaseBinaryName()
	if err != nil {
		return err
	}
	baseURL := "https://github.com/" + releaseRepository + "/releases/latest/download"
	binaryData, err := download(ctx, baseURL+"/"+binary)
	if err != nil {
		return err
	}
	checksums, err := download(ctx, baseURL+"/checksums.txt")
	if err != nil {
		return err
	}
	if err := verifyChecksum(binary, binaryData, checksums); err != nil {
		return err
	}
	if err := replaceExecutable(binaryData); err != nil {
		return err
	}
	fmt.Printf("updated colimui to %s\n", latest)
	return nil
}

func releaseBinaryName() (string, error) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		return "", fmt.Errorf("updates are not supported on %s", runtime.GOOS)
	}
	if runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64" {
		return "", fmt.Errorf("updates are not supported on %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	return "colimui_" + runtime.GOOS + "_" + runtime.GOARCH, nil
}

func download(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download failed: %s", resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 100<<20))
}

func verifyChecksum(filename string, data, checksums []byte) error {
	var expected string
	for _, line := range strings.Split(string(checksums), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[len(fields)-1] == filename {
			expected = fields[0]
			break
		}
	}
	if expected == "" {
		return fmt.Errorf("no checksum found for %s", filename)
	}
	sum := sha256.Sum256(data)
	if !strings.EqualFold(expected, hex.EncodeToString(sum[:])) {
		return fmt.Errorf("checksum verification failed for %s", filename)
	}
	return nil
}

func replaceExecutable(data []byte) error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	target, err := replaceBinary(executable, data)
	if !errors.Is(err, fs.ErrPermission) {
		return err
	}
	fmt.Fprintf(os.Stderr, "%s is not writable by you, so sudo will replace it.\n"+
		"To update without a password, reinstall into ~/.local/bin and remove this copy:\n"+
		"  curl -fsSL %s | sh\n  sudo rm %s\n", filepath.Dir(target), installScriptURL, target)
	return sudoInstall(target, data)
}

// replaceBinary atomically swaps the binary behind path, following symlinks so
// a linked install keeps its link; it returns the resolved target.
func replaceBinary(path string, data []byte) (string, error) {
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return path, err
	}
	return target, writeFileAtomic(target, data, 0o755)
}

func sudoInstall(target string, data []byte) error {
	temporary, err := os.CreateTemp("", "colimui-update-*")
	if err != nil {
		return err
	}
	defer os.Remove(temporary.Name())
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	command := exec.Command("sudo", "install", "-m", "0755", temporary.Name(), target)
	command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("replace executable: %w", err)
	}
	return nil
}
