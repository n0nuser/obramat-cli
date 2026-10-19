package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultPaths(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(base, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(base, "data"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(base, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(base, "state"))
	t.Setenv("OBRAMAT_CONFIG", "")

	p := DefaultPaths()
	for what, got := range map[string]string{
		"config":  p.ConfigFile,
		"session": p.SessionFile,
		"state":   p.StateDB,
		"cache":   p.CacheDir,
		"logs":    p.StateDir,
		"chrome":  p.ChromeDir,
	} {
		if !strings.HasPrefix(got, base) {
			t.Errorf("%s = %q, outside the XDG home %q", what, got, base)
		}
	}
	if filepath.Base(p.SessionFile) != "session.json" || filepath.Base(p.ConfigFile) != "config.toml" {
		t.Fatalf("unexpected file names: %q %q", p.ConfigFile, p.SessionFile)
	}
	if filepath.Base(p.ChromeDir) != "chrome" {
		t.Fatalf("chrome dir %q", p.ChromeDir)
	}

	t.Setenv("OBRAMAT_CONFIG", filepath.Join(base, "elsewhere.toml"))
	if got := DefaultPaths().ConfigFile; got != filepath.Join(base, "elsewhere.toml") {
		t.Fatalf("OBRAMAT_CONFIG ignored: %q", got)
	}
}

func TestLoadSave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	cfg, err := Load(path)
	if err != nil || cfg.Store != 0 || len(cfg.Sinks) != 0 {
		t.Fatalf("missing file: %+v %v", cfg, err)
	}

	cfg = Config{Store: 22, Watch: WatchConfig{Interval: "1h"}, Sinks: map[string]SinkConfig{
		"phone": {Type: "ntfy", URL: "https://ntfy.sh", Topic: "t"},
	}}
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("config mode %v, want 0600", fi.Mode().Perm())
	}
	back, err := Load(path)
	if err != nil || back.Store != 22 || back.Watch.Interval != "1h" || back.Sinks["phone"].Topic != "t" {
		t.Fatalf("round trip: %+v %v", back, err)
	}
}

func TestLoadRejectsBadFiles(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"unknown key": "colour = 'red'\n",
		"broken toml": "store = [\n",
		"wrong type":  "store = 'four'\n",
	} {
		path := filepath.Join(dir, strings.ReplaceAll(name, " ", "-")+".toml")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(path); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

func TestWriteAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "deeper", "file.txt")
	if err := WriteAtomic(path, []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(filepath.Dir(path)); err != nil || fi.Mode().Perm() != 0o700 {
		t.Fatalf("parent dir: %v %v", fi.Mode().Perm(), err)
	}
	if err := WriteAtomic(path, []byte("two"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "two" {
		t.Fatalf("%q %v", got, err)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Fatalf("temp files left behind: %v", entries)
	}
}

// A path whose parent is a regular file cannot be written, and the error comes
// back rather than a partial file.
func TestWriteAtomicRefusesABadParent(t *testing.T) {
	dir := t.TempDir()
	parent := filepath.Join(dir, "file")
	if err := os.WriteFile(parent, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteAtomic(filepath.Join(parent, "child"), []byte("x"), 0o600); err == nil {
		t.Fatal("wrote under a regular file")
	}
}

func TestLoadRefusesADirectory(t *testing.T) {
	if _, err := Load(t.TempDir()); err == nil {
		t.Fatal("a directory read as a config")
	}
}

func TestSaveReportsWriteErrors(t *testing.T) {
	dir := t.TempDir()
	parent := filepath.Join(dir, "file")
	if err := os.WriteFile(parent, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Save(filepath.Join(parent, "config.toml"), Config{}); err == nil {
		t.Fatal("saved under a regular file")
	}
}

func TestLoadSessionErrors(t *testing.T) {
	p := testPaths(t)
	if err := os.MkdirAll(filepath.Dir(p.SessionFile), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.SessionFile, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSession(p); err == nil || !strings.Contains(err.Error(), "corrupt") {
		t.Fatalf("corrupt session: %v", err)
	}

	missing := filepath.Join(t.TempDir(), "missing.json")
	t.Setenv(CookieJarEnv, missing)
	if _, err := LoadSession(p); err == nil {
		t.Fatal("a missing env export was accepted")
	}
	foreign := filepath.Join(t.TempDir(), "foreign.json")
	if err := os.WriteFile(foreign, []byte(`[{"name":"x","value":"y","domain":".example.com"}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(CookieJarEnv, foreign)
	if _, err := LoadSession(p); err == nil {
		t.Fatal("an env export with no obramat cookies was accepted")
	}
}

// A session path that cannot be written reports the error.
func TestSaveSessionReportsWriteErrors(t *testing.T) {
	dir := t.TempDir()
	parent := filepath.Join(dir, "file")
	if err := os.WriteFile(parent, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	p := Paths{SessionFile: filepath.Join(parent, "session.json")}
	if err := SaveSession(p, &StoredSession{}); err == nil {
		t.Fatal("saved under a regular file")
	}
}
