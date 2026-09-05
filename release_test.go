package main

import (
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
