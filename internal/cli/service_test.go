package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/n0nuser/obramat-cli/internal/config"
)

func TestServiceHelpers(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the unit shapes below are the Linux ones")
	}
	t.Setenv("HOME", "/home/someone")
	t.Setenv("XDG_CONFIG_HOME", "/xdg/config")
	t.Setenv("XDG_DATA_HOME", "/xdg/data")
	t.Setenv("XDG_CACHE_HOME", "/xdg/cache")
	t.Setenv("XDG_STATE_HOME", "/xdg/state")
	t.Setenv("OBRAMAT_CONFIG", "relative.toml")
	t.Setenv("OBRAMAT_STORE", "4")

	platform, files := serviceFiles()
	if platform != "systemd" || len(files) != 2 || !strings.HasSuffix(files[0], ".service") || !strings.HasSuffix(files[1], ".timer") {
		t.Fatalf("%s %v", platform, files)
	}
	env := serviceEnv()
	if len(env) != 7 {
		t.Fatalf("env: %v", env)
	}
	if got := env[len(env)-2]; got[0] != "OBRAMAT_CONFIG" || !filepath.IsAbs(got[1]) {
		t.Fatalf("config path not absolute: %v", got)
	}

	service, timer := systemdUnits("/bin/obramat", "/tmp/log%x", 90*time.Minute, [][2]string{{"HOME", "/home/me"}})
	for _, want := range []string{"ExecStart=\"/bin/obramat\" watch check --format jsonl", "Environment=\"HOME=/home/me\"", "StandardOutput=append:/tmp/log%%x"} {
		if !strings.Contains(service, want) {
			t.Errorf("service unit missing %q:\n%s", want, service)
		}
	}
	if !strings.Contains(timer, "OnUnitActiveSec=5400s") {
		t.Errorf("timer: %s", timer)
	}
	if got := systemdEscape(`a"b\c%`); got != `"a\"b\\c%%"` {
		t.Fatalf("systemdEscape = %s", got)
	}

	plist := launchdPlist("/bin/obramat", "/tmp/log", time.Hour, [][2]string{{"HOME", "/home/me"}})
	for _, want := range []string{"<string>/bin/obramat</string>", "<key>HOME</key><string>/home/me</string>", "<key>StartInterval</key><integer>3600</integer>"} {
		if !strings.Contains(plist, want) {
			t.Errorf("plist missing %q:\n%s", want, plist)
		}
	}
	if got := xmlText(`a&b<c>"`); got != `a&amp;b&lt;c&gt;&quot;` {
		t.Fatalf("xmlText = %s", got)
	}

	if got := schtasksLine(`C:\obramat.exe`, 90*time.Minute); !strings.Contains(got, "/MO 90") || !strings.Contains(got, `C:\obramat.exe`) {
		t.Fatalf("schtasks line: %s", got)
	}
	if got := checkArgs(); strings.Join(got, " ") != "watch check --format jsonl" {
		t.Fatalf("checkArgs = %v", got)
	}
}

func TestInstalledAndServiceState(t *testing.T) {
	dir := t.TempDir()
	files := []string{filepath.Join(dir, "a"), filepath.Join(dir, "b")}
	if installed(files) {
		t.Fatal("missing files count as installed")
	}
	for _, f := range files {
		if err := os.WriteFile(f, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if !installed(files) {
		t.Fatal("present files count as missing")
	}
	if state, _ := serviceState("launchd", files); state != "not loaded" {
		t.Fatalf("launchd state = %q", state)
	}
	if state, _ := serviceState("systemd", files); state == "not installed" || state == "" {
		t.Fatalf("systemd state = %q", state)
	}
}

func TestServiceViewPretty(t *testing.T) {
	var b bytes.Buffer
	serviceView{Platform: "systemd", Unit: "obramat-watch.timer", Files: []string{"/a", "/b"},
		State: "enabled", LastRun: "today", Log: "/l", Command: "cmd", Hint: "linger"}.Pretty(&b, false)
	out := b.String()
	for _, want := range []string{"systemd", "obramat-watch.timer", "enabled", "linger"} {
		if !strings.Contains(out, want) {
			t.Errorf("pretty missing %q:\n%s", want, out)
		}
	}
}

func TestRotateServiceLog(t *testing.T) {
	dir := t.TempDir()
	a := &App{Paths: config.Paths{StateDir: dir}}
	a.rotateServiceLog() // no log yet: a no-op
	log := filepath.Join(dir, "obramat-watch.log")
	if err := os.WriteFile(log, bytes.Repeat([]byte("x"), 1<<20+1), 0o600); err != nil {
		t.Fatal(err)
	}
	a.rotateServiceLog()
	if _, err := os.Stat(log + ".1"); err != nil {
		t.Fatalf("log not rotated: %v", err)
	}
	small := filepath.Join(dir, "small.log")
	if err := os.WriteFile(small, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	a.rotateServiceLog()
}

func TestAskUpdate(t *testing.T) {
	for in, want := range map[string]bool{"y\n": true, "yes\n": true, "\n": true, "n\n": false, "": false} {
		var errb bytes.Buffer
		a := &App{Stdin: strings.NewReader(in), Stderr: &errb}
		if got := a.askUpdate("v1.2.3"); got != want {
			t.Errorf("askUpdate(%q) = %v", in, got)
		}
		if !strings.Contains(errb.String(), "Update?") {
			t.Errorf("no prompt for %q", in)
		}
	}
}

func TestMaybeUpdateSkips(t *testing.T) {
	root := CommandTree("1.2.3")
	find := func(name string) *cobra.Command {
		c, _, err := root.Find([]string{name})
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	a := &App{Version: "1.2.3", Interactive: true, Paths: config.Paths{CacheDir: t.TempDir()}, Client: nil}
	ctx := context.Background()
	// Excluded commands, an unstable build and the env switch all return at once.
	for _, name := range []string{"update", "mcp", "watch"} {
		a.maybeUpdate(ctx, find(name))
	}
	a.Version = "dev"
	a.maybeUpdate(ctx, find("search"))
	a.Version = "1.2.3"
	t.Setenv("OBRAMAT_NO_UPDATE_CHECK", "1")
	a.maybeUpdate(ctx, find("search"))
	// A fresh stamp means no check either.
	t.Setenv("OBRAMAT_NO_UPDATE_CHECK", "")
	stamp := filepath.Join(a.Paths.CacheDir, "update-check")
	if err := os.WriteFile(stamp, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	a.maybeUpdate(ctx, find("search"))
	a.Interactive = false
	a.maybeUpdate(ctx, find("search"))
}
