package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/n0nuser/obramat-cli/internal/api"
)

// CookieJarEnv names a cookie export (any format `auth login` reads) to use
// instead of the stored session. It is read on every run and never copied to
// disk, cookie rotations included.
const CookieJarEnv = "OBRAMAT_COOKIEJAR"

// Session sources.
const (
	SourceFile = "file"
	SourceEnv  = "env"
)

// StoredSession is the browser session obramat replays: the imported cookies
// and the user agent of the browser that earned them, plus where they came
// from. It is stored 0600; values never reach stdout.
type StoredSession struct {
	api.Session
	ImportedAt   time.Time `json:"imported_at"`
	ImportFormat string    `json:"import_format"`

	// Source says where this session came from at runtime. Env sessions are
	// never saved: SaveSession refuses them.
	Source string `json:"-"`
	Path   string `json:"-"`
}

// LoadSession returns the session: OBRAMAT_COOKIEJAR when set, else the
// stored file. (nil, nil) means there is none.
func LoadSession(p Paths) (*StoredSession, error) {
	if path := os.Getenv(CookieJarEnv); path != "" {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", CookieJarEnv, err)
		}
		res, err := api.ParseExport(raw, api.FormatAuto)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", CookieJarEnv, err)
		}
		return &StoredSession{Session: res.Session, ImportFormat: res.Format, Source: SourceEnv, Path: path}, nil
	}
	raw, err := os.ReadFile(p.SessionFile)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var s StoredSession
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("%s is corrupt (%v). Run `obramat auth login` again", p.SessionFile, err)
	}
	s.Source, s.Path = SourceFile, p.SessionFile
	return &s, nil
}

// SaveSession writes the session file, 0600, atomically.
func SaveSession(p Paths, s *StoredSession) error {
	if s.Source == SourceEnv {
		return fmt.Errorf("refusing to write a %s session to disk", CookieJarEnv)
	}
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := WriteAtomic(p.SessionFile, raw, 0o600); err != nil {
		return err
	}
	s.Source, s.Path = SourceFile, p.SessionFile
	return nil
}

// RemoveSession deletes the stored session file. Missing is not an error.
func RemoveSession(p Paths) (bool, error) {
	err := os.Remove(p.SessionFile)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}
