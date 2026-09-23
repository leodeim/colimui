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
