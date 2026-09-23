package main

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestCommandOutputIncludesStderr(t *testing.T) {
	_, err := commandOutput("", time.Second, "sh", "-c", "echo out; echo 'daemon unreachable' >&2; exit 3")
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 3 {
		t.Fatalf("error = %v, want wrapped exit status 3", err)
	}
	if !strings.Contains(err.Error(), "daemon unreachable") {
		t.Fatalf("error %q is missing stderr", err)
	}
}

func TestCommandOutputTimesOut(t *testing.T) {
	start := time.Now()
	_, err := commandOutput("", 50*time.Millisecond, "sleep", "5")
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("error = %v, want timeout", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("timeout took %v", elapsed)
	}
}

func TestCommandOutputSuccess(t *testing.T) {
	out, err := commandOutput("", time.Second, "sh", "-c", "echo hi; echo noise >&2")
	if err != nil || strings.TrimSpace(string(out)) != "hi" {
		t.Fatalf("output = %q, %v", out, err)
	}
}

func stubColimaStatus(t *testing.T, fn func(string) ([]byte, error)) *int {
	t.Helper()
	calls := 0
	previous := colimaStatus
	colimaStatus = func(name string) ([]byte, error) {
		calls++
		return fn(name)
	}
	dockerHosts.Lock()
	dockerHosts.byProfile = map[string]string{}
	dockerHosts.Unlock()
	t.Cleanup(func() {
		colimaStatus = previous
		dockerHosts.Lock()
		dockerHosts.byProfile = map[string]string{}
		dockerHosts.Unlock()
	})
	return &calls
}

func lastEnv(env []string, key string) string {
	value := ""
	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok && k == key {
			value = v
		}
	}
	return value
}

func TestDockerEnvUsesColimaSocket(t *testing.T) {
	calls := stubColimaStatus(t, func(name string) ([]byte, error) {
		return []byte(`{"display_name":"colima","docker_socket":"unix:///home/u/.colima/` + name + `/docker.sock"}`), nil
	})
	for range 2 {
		if got := lastEnv(dockerEnv("dev"), "DOCKER_HOST"); got != "unix:///home/u/.colima/dev/docker.sock" {
			t.Fatalf("DOCKER_HOST = %q", got)
		}
	}
	if *calls != 1 {
		t.Fatalf("colima status ran %d times, want 1 (cached)", *calls)
	}
}

func TestDockerEnvFallsBackToContext(t *testing.T) {
	for name, fn := range map[string]func(string) ([]byte, error){
		"not running": func(string) ([]byte, error) { return nil, errors.New("colima is not running") },
		"no socket":   func(string) ([]byte, error) { return []byte(`{"runtime":"containerd"}`), nil },
		"bad json":    func(string) ([]byte, error) { return []byte("nope"), nil },
	} {
		t.Run(name, func(t *testing.T) {
			calls := stubColimaStatus(t, fn)
			for range 2 {
				env := dockerEnv("dev")
				if got := lastEnv(env, "DOCKER_CONTEXT"); got != "colima-dev" {
					t.Fatalf("DOCKER_CONTEXT = %q, want colima-dev", got)
				}
			}
			if *calls != 2 {
				t.Fatalf("colima status ran %d times, want 2 (failures not cached)", *calls)
			}
		})
	}
}
