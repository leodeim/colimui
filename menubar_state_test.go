package main

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"testing"
	"time"
)

func TestMenubarTitle(t *testing.T) {
	for _, test := range []struct {
		name     string
		profiles []profile
		want     string
	}{
		{"no profiles", nil, ""},
		{"stopped", []profile{{Name: "default", Status: "Stopped"}}, ""},
		{"running", []profile{{Name: "default", Status: "Running"}}, ""},
		{"multiple running", []profile{{Name: "a", Status: "Running"}, {Name: "b", Status: "running"}}, "2"},
		{"mixed", []profile{{Name: "a", Status: "Running"}, {Name: "b", Status: "Stopped"}}, ""},
	} {
		if got := menubarTitle(test.profiles); got != test.want {
			t.Errorf("%s: menubarTitle() = %q, want %q", test.name, got, test.want)
		}
	}
}

func TestDimmedIcon(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	src.SetNRGBA(0, 0, color.NRGBA{A: 255})
	src.SetNRGBA(1, 0, color.NRGBA{A: 0})
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatal(err)
	}
	dimmed, err := png.Decode(bytes.NewReader(dimmedIcon(buf.Bytes())))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, a := dimmed.At(0, 0).RGBA(); a>>8 != 255*2/5 {
		t.Errorf("opaque pixel alpha = %d, want %d", a>>8, 255*2/5)
	}
	if _, _, _, a := dimmed.At(1, 0).RGBA(); a != 0 {
		t.Errorf("transparent pixel alpha = %d, want 0", a)
	}
	if got := dimmedIcon([]byte("not a png")); string(got) != "not a png" {
		t.Error("invalid input should come back unchanged")
	}
}

func TestMenubarProfileLine(t *testing.T) {
	if got, want := menubarProfileLine(profile{Name: "default", Status: "Running"}), "default — Running"; got != want {
		t.Errorf("menubarProfileLine() = %q, want %q", got, want)
	}
	if got, want := menubarProfileLine(profile{Name: "work"}), "work — Unknown"; got != want {
		t.Errorf("menubarProfileLine() = %q, want %q", got, want)
	}
}

func TestMenubarProfileDetails(t *testing.T) {
	p := profile{Name: "default", CPUs: 2, Memory: 2 << 30, Disk: 60 << 30, Arch: "aarch64"}
	if got, want := menubarProfileDetails(p), "2 cpu · 2.0g ram · 60g disk · aarch64"; got != want {
		t.Errorf("menubarProfileDetails() = %q, want %q", got, want)
	}
}

func TestMenubarSignature(t *testing.T) {
	profiles := []profile{{Name: "default", Status: "Running", CPUs: 2, Memory: 1024, Disk: 2048}}
	same := []profile{{Name: "default", Status: "Running", CPUs: 2, Memory: 1024, Disk: 2048}}
	if menubarSignature(profiles) != menubarSignature(same) {
		t.Error("identical profiles should produce identical signatures")
	}
	stopped := []profile{{Name: "default", Status: "Stopped", CPUs: 2, Memory: 1024, Disk: 2048}}
	if menubarSignature(profiles) == menubarSignature(stopped) {
		t.Error("status change should change the signature")
	}
	if menubarSignature(nil) != "" {
		t.Errorf("menubarSignature(nil) = %q, want empty", menubarSignature(nil))
	}
}

func TestIdleTrackerObserve(t *testing.T) {
	var tracker idleTracker
	now := time.Now()
	after := 30 * time.Minute

	if tracker.observe("default", false, now, after) {
		t.Fatal("first idle observation must arm the timer, not fire")
	}
	if _, tracked := tracker.remaining("default", now, after); !tracked {
		t.Fatal("armed profile should report a countdown")
	}
	if tracker.observe("default", false, now.Add(29*time.Minute), after) {
		t.Fatal("fired before the window elapsed")
	}
	if !tracker.observe("default", false, now.Add(31*time.Minute), after) {
		t.Fatal("did not fire after the window elapsed")
	}
	if tracker.observe("default", false, now.Add(31*time.Minute), after) {
		t.Fatal("fired twice for one elapsed window")
	}
}

func TestIdleTrackerActivityAndClearReset(t *testing.T) {
	var tracker idleTracker
	now := time.Now()
	after := time.Minute

	tracker.observe("default", false, now, after)
	tracker.observe("other", false, now, after)
	if tracker.observe("default", true, now.Add(2*time.Minute), after) {
		t.Fatal("active containers must reset, not fire")
	}
	tracker.clear("other")
	if tracker.observe("default", false, now.Add(3*time.Minute), after) {
		t.Fatal("reset profile fired without a fresh idle window")
	}
	if _, tracked := tracker.remaining("other", now, after); tracked {
		t.Fatal("cleared profile still reports a countdown")
	}
}

func TestAutoStopStateLabel(t *testing.T) {
	for _, test := range []struct {
		name string
		auto autoStopState
		want string
	}{
		{"enabled", autoStopState{after: 30 * time.Minute, enabled: true}, "disable idle auto-stop (30m)"},
		{"disabled", autoStopState{after: 30 * time.Minute}, "enable idle auto-stop (30m)"},
		{"env on", autoStopState{after: 45 * time.Minute, enabled: true, env: true}, "idle auto-stop: 45m (COLIMUI_AUTO_STOP)"},
		{"env off", autoStopState{after: 30 * time.Minute, env: true}, "idle auto-stop: off (COLIMUI_AUTO_STOP)"},
		{"broken", autoStopState{err: errors.New("boom")}, "idle auto-stop: invalid setting"},
	} {
		if got := test.auto.label(); got != test.want {
			t.Errorf("%s: label() = %q, want %q", test.name, got, test.want)
		}
	}
}

func TestResolveMenubarAutoStop(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(autoStopEnv, "")

	auto := resolveMenubarAutoStop()
	if !auto.enabled || auto.after != autoStopDefault || auto.env || auto.err != nil {
		t.Fatalf("default resolve = %+v", auto)
	}

	if err := updateSettings(settingsPath(), func(s *settings) { s.AutoStop = "off" }); err != nil {
		t.Fatal(err)
	}
	if auto := resolveMenubarAutoStop(); auto.enabled {
		t.Fatalf("saved off resolve = %+v", auto)
	}

	t.Setenv(autoStopEnv, "45m")
	auto = resolveMenubarAutoStop()
	if !auto.enabled || auto.after != 45*time.Minute || !auto.env {
		t.Fatalf("env resolve = %+v", auto)
	}

	t.Setenv(autoStopEnv, "nonsense")
	if auto := resolveMenubarAutoStop(); auto.err == nil || auto.enabled {
		t.Fatalf("invalid env resolve = %+v", auto)
	}
}

func TestAppleScriptQuote(t *testing.T) {
	for _, test := range []struct {
		in   string
		want string
	}{
		{`plain`, `"plain"`},
		{`with "quotes"`, `"with \"quotes\""`},
		{`back\slash`, `"back\\slash"`},
	} {
		if got := appleScriptQuote(test.in); got != test.want {
			t.Errorf("appleScriptQuote(%q) = %q, want %q", test.in, got, test.want)
		}
	}
}
