// Package config owns what the CLI keeps on disk outside the watch state:
// where files live, config.toml, the imported browser session (0600), and the
// atomic write every file goes through. There are no account credentials: the
// session is the user's own browser cookies, and no password is ever asked
// for.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/adrg/xdg"
	"github.com/pelletier/go-toml/v2"
)

const appDir = "obramat-cli"

// Paths are resolved per call so tests and service units can move them
// through the environment.
type Paths struct {
	ConfigFile  string // $XDG_CONFIG_HOME/obramat-cli/config.toml, or OBRAMAT_CONFIG
	SessionFile string // $XDG_CONFIG_HOME/obramat-cli/session.json: the imported browser session, 0600
	StateDB     string // $XDG_DATA_HOME/obramat-cli/state.db: watches, seen products, events
	CacheDir    string // $XDG_CACHE_HOME/obramat-cli: store list cache, update check
	StateDir    string // $XDG_STATE_HOME/obramat-cli: scheduler logs
}

func DefaultPaths() Paths {
	// adrg/xdg reads the environment once at init; reload so a variable set
	// by a test or a service unit counts.
	xdg.Reload()
	p := Paths{
		ConfigFile:  filepath.Join(xdg.ConfigHome, appDir, "config.toml"),
		SessionFile: filepath.Join(xdg.ConfigHome, appDir, "session.json"),
		StateDB:     filepath.Join(xdg.DataHome, appDir, "state.db"),
		CacheDir:    filepath.Join(xdg.CacheHome, appDir),
		StateDir:    filepath.Join(xdg.StateHome, appDir),
	}
	if v := os.Getenv("OBRAMAT_CONFIG"); v != "" {
		p.ConfigFile = v
	}
	return p
}

// SinkConfig is one place events go besides stdout. Type picks the fields:
// ntfy uses URL+Topic(+TokenFile), webhook URL(+Template), exec Command.
type SinkConfig struct {
	Type      string   `toml:"type" json:"type"`
	URL       string   `toml:"url,omitempty" json:"url,omitempty"`
	Topic     string   `toml:"topic,omitempty" json:"topic,omitempty"`
	TokenFile string   `toml:"token_file,omitempty" json:"token_file,omitempty"`
	Template  string   `toml:"template,omitempty" json:"template,omitempty"`
	Command   []string `toml:"command,omitempty" json:"command,omitempty"`
}

type WatchConfig struct {
	Interval string `toml:"interval,omitempty" json:"interval,omitempty"`
}

type Config struct {
	// Store is the default store code for prices and stock (`obramat store
	// list`). OBRAMAT_STORE and --store override it.
	Store int                   `toml:"store,omitempty" json:"store,omitempty"`
	Watch WatchConfig           `toml:"watch,omitempty" json:"watch,omitempty"`
	Sinks map[string]SinkConfig `toml:"sinks,omitempty" json:"sinks,omitempty"`
}

// Load reads config.toml. A missing file is an empty config; an unknown key
// is an error, so a typo does not silently do nothing.
func Load(path string) (Config, error) {
	var cfg Config
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	dec := toml.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return cfg, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

func Save(path string, cfg Config) error {
	raw, err := toml.Marshal(cfg)
	if err != nil {
		return err
	}
	return WriteAtomic(path, raw, 0o600) // sink URLs can carry tokens
}

// WriteAtomic writes through a temp file in the same directory and renames it
// into place, so a crash never leaves half a file. The mode is set on the
// temp file and again on the result, whatever the umask.
func WriteAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name) // no-op after the rename
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	return os.Chmod(path, mode)
}
