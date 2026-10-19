package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestBinaryContract builds the executable and checks what only exists at
// process level: --version, --help, exit codes reaching the shell, and that
// errors go to stderr while stdout stays clean. No network is used.
func TestBinaryContract(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary")
	}
	bin := filepath.Join(t.TempDir(), "obramat")
	if out, err := exec.Command("go", "build", "-ldflags", "-X main.version=9.9.9", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	env := append(os.Environ(), "XDG_CONFIG_HOME="+t.TempDir(), "XDG_DATA_HOME="+t.TempDir(), "XDG_CACHE_HOME="+t.TempDir(), "XDG_STATE_HOME="+t.TempDir(),
		"OBRAMAT_BASE_URL=http://127.0.0.1:1", "OBRAMAT_STORE=", "OBRAMAT_CONFIG=")
	run := func(args ...string) (string, string, int) {
		cmd := exec.Command(bin, args...)
		cmd.Env = env
		var out, errb strings.Builder
		cmd.Stdout, cmd.Stderr = &out, &errb
		err := cmd.Run()
		if ee, ok := err.(*exec.ExitError); ok {
			return out.String(), errb.String(), ee.ExitCode()
		} else if err != nil {
			t.Fatal(err)
		}
		return out.String(), errb.String(), 0
	}
	if out, _, code := run("--version"); code != 0 || out != "obramat 9.9.9\n" {
		t.Fatalf("--version: %d %q", code, out)
	}
	if out, _, code := run("--help"); code != 0 || !strings.Contains(out, "Examples:") {
		t.Fatalf("--help: %d", code)
	}
	if out, errOut, code := run("search", "--bogus"); code != 2 || out != "" || !strings.Contains(errOut, "unknown flag") {
		t.Fatalf("bad flag: %d %q %q", code, out, errOut)
	}
	if _, errOut, code := run("search", "x", "--error-format", "json"); code != 6 || !strings.Contains(errOut, `"category":"network"`) {
		t.Fatalf("unreachable site: %d %q", code, errOut)
	}
	if _, _, code := run("search", "x", "--category", "/madera/"); code != 2 {
		t.Fatalf("keywords plus category: %d", code)
	}
	if out, _, code := run("watch", "list"); code != 0 || strings.TrimSpace(out) != "[]" {
		t.Fatalf("watch list: %d %q", code, out)
	}
	if out, _, code := run("auth", "status"); code != 0 || !strings.Contains(out, `"source": "none"`) {
		t.Fatalf("auth status: %d %q", code, out)
	}
	if out, _, code := run("auth", "logout"); code != 0 || !strings.Contains(out, `"removed": false`) {
		t.Fatalf("auth logout: %d %q", code, out)
	}
	if out, _, code := run("completion", "bash"); code != 0 || !strings.Contains(out, "obramat") {
		t.Fatalf("completion: %d", code)
	}
}
