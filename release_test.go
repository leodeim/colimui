package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsNewerVersion(t *testing.T) {
	for _, test := range []struct {
		candidate string
		current   string
		want      bool
	}{
		{"v0.0.2", "v0.0.1", true},
		{"v0.1.0", "v0.0.9", true},
		{"v1.0.0", "v1.0.0", false},
		{"v0.0.1", "v0.0.2", false},
		{"latest", "v0.0.1", false},
		{"v0.0.2", "dev", false},
	} {
		if got := isNewerVersion(test.candidate, test.current); got != test.want {
			t.Errorf("isNewerVersion(%q, %q) = %t, want %t", test.candidate, test.current, got, test.want)
		}
	}
}

func TestVerifyChecksum(t *testing.T) {
	data := []byte("hello")
	checksums := "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824  colimui_darwin_arm64\n"
	if err := verifyChecksum("colimui_darwin_arm64", data, []byte(checksums)); err != nil {
		t.Fatal(err)
	}
	if err := verifyChecksum("colimui_linux_amd64", data, []byte(checksums)); err == nil {
		t.Fatal("expected missing checksum error")
	}
}

func TestUpdateNoticeIsShown(t *testing.T) {
	m := model{width: 100, height: 24, status: "ready"}
	updated, command := m.Update(updateCheckMsg{version: "v0.0.2"})
	if command != nil {
		t.Fatal("update check should not return a command")
	}
	view := updated.(model).View()
	if !strings.Contains(view, "update available: v0.0.2") || !strings.Contains(view, "run colimui update") {
		t.Fatalf("update notice missing from view: %q", view)
	}
}

func TestReplaceBinaryFollowsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "colimui-bin")
	link := filepath.Join(dir, "colimui")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	got, err := replaceBinary(link, []byte("new"))
	if err != nil {
		t.Fatal(err)
	}
	resolvedTarget, _ := filepath.EvalSymlinks(target)
	if got != resolvedTarget {
		t.Fatalf("replaced %q, want %q", got, resolvedTarget)
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("link was replaced by a regular file: %v %v", info, err)
	}
	data, err := os.ReadFile(link)
	if err != nil || string(data) != "new" {
		t.Fatalf("content via link = %q, %v", data, err)
	}
	if info, _ := os.Stat(target); info.Mode().Perm() != 0o755 {
		t.Fatalf("mode = %v, want 0755", info.Mode().Perm())
	}
}

func TestReplaceBinaryReportsPermissionWithoutLeftovers(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write read-only directories")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "colimui")
	if err := os.WriteFile(path, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })
	if _, err := replaceBinary(path, []byte("new")); !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("error = %v, want permission error", err)
	}
	entries, _ := os.ReadDir(dir)
	if data, _ := os.ReadFile(path); len(entries) != 1 || string(data) != "old" {
		t.Fatalf("dir entries %v, content %q", entries, data)
	}
}
