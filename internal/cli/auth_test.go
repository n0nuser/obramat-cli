package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sessionPath mirrors config.DefaultPaths under the isolated XDG home.
func sessionPath() string {
	return filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "obramat-cli", "session.json")
}

const syntheticExport = `[
  {"name":"datadome","value":"SYNTHETIC_DD","domain":".obramat.es","path":"/","secure":true},
  {"name":"customer_context","value":"SYNTHETIC_STORE","domain":".obramat.es","path":"/"}
]`

func TestAuthLoginStatusLogout(t *testing.T) {
	isolate(t, "http://127.0.0.1:1")
	// No session yet.
	out, _, code := run("auth", "status")
	if code != 0 || !strings.Contains(out, `"source": "none"`) {
		t.Fatalf("status before login: %d %q", code, out)
	}
	file := filepath.Join(t.TempDir(), "cookies.json")
	if err := os.WriteFile(file, []byte(syntheticExport), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, errOut, code := run("auth", "login", "--cookies", file, "--no-check"); code != 0 {
		t.Fatalf("login: %d %q %q", code, out, errOut)
	}
	fi, err := os.Stat(sessionPath())
	if err != nil {
		t.Fatalf("session file: %v", err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("session mode %v, want 0600", fi.Mode().Perm())
	}
	out, _, code = run("auth", "status")
	if code != 0 || !strings.Contains(out, "SYNTHETIC_DD") && !strings.Contains(out, "datadome") {
		t.Fatalf("status after login: %d %q", code, out)
	}
	if strings.Contains(out, "SYNTHETIC_STORE") {
		t.Fatalf("status leaked a cookie value: %q", out)
	}
	if out, _, code := run("auth", "logout"); code != 0 || !strings.Contains(out, `"removed": true`) {
		t.Fatalf("logout: %d %q", code, out)
	}
	if _, err := os.Stat(sessionPath()); !os.IsNotExist(err) {
		t.Fatalf("session file survived logout: %v", err)
	}
}

// An env-sourced export is used, never stored, and login refuses to shadow it.
func TestAuthLoginRefusesWhenEnvJarIsSet(t *testing.T) {
	isolate(t, "http://127.0.0.1:1")
	file := filepath.Join(t.TempDir(), "cookies.json")
	if err := os.WriteFile(file, []byte(syntheticExport), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OBRAMAT_COOKIEJAR", file)
	if _, errOut, code := run("auth", "login", "--cookies", file, "--no-check"); code != 2 || !strings.Contains(errOut, "OBRAMAT_COOKIEJAR") {
		t.Fatalf("login with a jar in the env: %d %q", code, errOut)
	}
	if out, _, code := run("auth", "status"); code != 0 || !strings.Contains(out, `"source": "env"`) {
		t.Fatalf("env status: %d %q", code, out)
	}
}

func TestAuthLoginRejectsAFileWithOnlyForeignCookies(t *testing.T) {
	isolate(t, "http://127.0.0.1:1")
	file := filepath.Join(t.TempDir(), "cookies.json")
	if err := os.WriteFile(file, []byte(`[{"name":"x","value":"y","domain":".example.com"}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, errOut, code := run("auth", "login", "--cookies", file, "--no-check"); code != 2 || !strings.Contains(errOut, "no obramat.es cookies") {
		t.Fatalf("foreign-only export: %d %q", code, errOut)
	}
}
