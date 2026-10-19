package harvest

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

// cookieStoreCandidates are the cookie stores Chrome has used, oldest layout
// first. Only these, their journal siblings and the key reference are copied.
var cookieStoreCandidates = []string{
	filepath.Join("Default", "Cookies"),
	filepath.Join("Default", "Network", "Cookies"),
}

// DefaultProfileDirs are the profiles to look in, in order, for this OS. A
// directory counts when it holds a cookie store (see FindProfile).
func DefaultProfileDirs() []string {
	var out []string
	switch runtime.GOOS {
	case "darwin":
		if home, err := os.UserHomeDir(); err == nil {
			out = append(out,
				filepath.Join(home, "Library", "Application Support", "Google", "Chrome"),
				filepath.Join(home, "Library", "Application Support", "Chromium"),
			)
		}
	case "windows":
		for _, base := range []string{os.Getenv("LOCALAPPDATA"), os.Getenv("APPDATA")} {
			if base == "" {
				continue
			}
			out = append(out,
				filepath.Join(base, "Google", "Chrome", "User Data"),
				filepath.Join(base, "Chromium", "User Data"),
			)
		}
	default:
		var bases []string
		if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
			bases = append(bases, x)
		}
		if home, err := os.UserHomeDir(); err == nil {
			bases = append(bases, filepath.Join(home, ".config"))
		}
		for _, b := range bases {
			out = append(out,
				filepath.Join(b, "google-chrome"),
				filepath.Join(b, "chromium"),
			)
		}
	}
	return out
}

// FindProfile picks the real profile to harvest from: the override when set
// (it must have a cookie store), else the first of dirs that does. The Chrome
// 136+ restriction is why the caller copies from here instead of attaching to
// the running browser: a default profile ignores --remote-debugging-port.
func FindProfile(override string, dirs []string, isFile func(string) bool) (string, error) {
	hasStore := func(dir string) bool {
		for _, store := range cookieStoreCandidates {
			if isFile(filepath.Join(dir, store)) {
				return true
			}
		}
		return false
	}
	if override != "" {
		if hasStore(override) {
			return override, nil
		}
		return "", fmt.Errorf("%q has no Chrome cookie store (%s)", override, filepath.Join(override, cookieStoreCandidates[0]))
	}
	for _, dir := range dirs {
		if hasStore(dir) {
			return dir, nil
		}
	}
	return "", errors.New("no Chrome profile with cookies found; a fresh window will open instead")
}

// CopyPlan lists the files to copy into the temp profile: the cookie store
// (+journal/-wal/-shm) and Local State, which Chrome needs to decrypt the
// cookies with the same user's keyring, plus Default/Preferences so the copy
// looks like a real profile. Nothing else — no history, no other sites'
// storage. It errors when there is no cookie store to copy.
func CopyPlan(profile string) ([]string, error) {
	var plan []string
	add := func(rel string) bool {
		if isFile(filepath.Join(profile, rel)) {
			plan = append(plan, rel)
			return true
		}
		return false
	}
	add("Local State")
	add(filepath.Join("Default", "Preferences"))
	found := false
	for _, store := range cookieStoreCandidates {
		if add(store) {
			found = true
		}
		for _, suffix := range []string{"-journal", "-wal", "-shm"} {
			add(store + suffix)
		}
	}
	if !found {
		return nil, fmt.Errorf("%s has no cookie store to copy (%s)", profile, filepath.Join(profile, cookieStoreCandidates[0]))
	}
	return plan, nil
}

// CopyProfileFiles copies the planned files into dst, creating every directory
// 0700 and each file 0600. dst is the temp profile the browser runs from.
func CopyProfileFiles(profile, dst string, files []string) error {
	if err := os.MkdirAll(dst, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(dst, 0o700); err != nil {
		return err
	}
	for _, rel := range files {
		if err := copyFile(filepath.Join(profile, rel), filepath.Join(dst, rel)); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Chmod(dst, 0o600)
}
