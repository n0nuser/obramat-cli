package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/n0nuser/obramat-cli/internal/api"
)

func testPaths(t *testing.T) Paths {
	dir := t.TempDir()
	return Paths{ConfigFile: filepath.Join(dir, "config.toml"), SessionFile: filepath.Join(dir, "obramat-cli", "session.json")}
}

func TestSessionFileIsPrivate(t *testing.T) {
	p := testPaths(t)
	s := &StoredSession{Session: api.Session{Cookies: []api.Cookie{{Name: "datadome", Value: "v", Domain: ".obramat.es"}}}}
	if err := SaveSession(p, s); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(p.SessionFile)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("session mode %v, want 0600", fi.Mode().Perm())
	}
	di, _ := os.Stat(filepath.Dir(p.SessionFile))
	if di.Mode().Perm() != 0o700 {
		t.Fatalf("session dir mode %v, want 0700", di.Mode().Perm())
	}
	back, err := LoadSession(p)
	if err != nil || back == nil || back.Source != SourceFile {
		t.Fatalf("load: %+v %v", back, err)
	}
	if c, _ := back.Cookie("datadome"); c.Value != "v" {
		t.Fatalf("cookie lost: %+v", back.Cookies)
	}
}

// Secrets from the environment must never land on disk, rotations included.
func TestEnvSessionIsNeverSaved(t *testing.T) {
	p := testPaths(t)
	jar := filepath.Join(t.TempDir(), "jar.txt")
	os.WriteFile(jar, []byte("datadome=fromenv"), 0o600)
	t.Setenv(CookieJarEnv, jar)
	s, err := LoadSession(p)
	if err != nil || s == nil || s.Source != SourceEnv {
		t.Fatalf("env session: %+v %v", s, err)
	}
	if err := SaveSession(p, s); err == nil {
		t.Fatal("an env session was written")
	}
	if _, err := os.Stat(p.SessionFile); !os.IsNotExist(err) {
		t.Fatal("session file exists after refusing an env session")
	}
}

func TestRemoveSession(t *testing.T) {
	p := testPaths(t)
	if removed, err := RemoveSession(p); err != nil || removed {
		t.Fatalf("missing session: removed=%v err=%v", removed, err)
	}
	if err := SaveSession(p, &StoredSession{}); err != nil {
		t.Fatal(err)
	}
	if removed, err := RemoveSession(p); err != nil || !removed {
		t.Fatalf("remove: removed=%v err=%v", removed, err)
	}
}
