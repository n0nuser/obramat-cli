// Package updater finds the latest GitHub release and replaces the running
// binary with its archive, after checking the archive against checksums.txt.
package updater

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	latestURL   = "https://api.github.com/repos/n0nuser/obramat-cli/releases/latest"
	downloadURL = "https://github.com/n0nuser/obramat-cli/releases/download/"
	maxDownload = 64 << 20
)

// ErrNoRelease means the repository has published no stable release yet.
var ErrNoRelease = errors.New("no release has been published yet")

var semver = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)$`)

// Stable reports whether v is a plain release version (no dev build, no
// prerelease suffix). Only those are ever replaced.
func Stable(v string) bool { return semver.MatchString(v) }

// Newer reports whether latest is a higher version than current.
func Newer(latest, current string) bool {
	l, c := semver.FindStringSubmatch(latest), semver.FindStringSubmatch(current)
	if l == nil || c == nil {
		return false
	}
	for i := 1; i <= 3; i++ {
		a, _ := strconv.Atoi(l[i])
		b, _ := strconv.Atoi(c[i])
		if a != b {
			return a > b
		}
	}
	return false
}

func Client() *http.Client { return &http.Client{Timeout: 60 * time.Second} }

func fetch(ctx context.Context, hc *http.Client, url string, limit int64) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", "obramat-cli-updater")
	resp, err := hc.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	if int64(len(body)) > limit {
		return nil, resp.StatusCode, fmt.Errorf("%s is larger than expected", url)
	}
	return body, resp.StatusCode, nil
}

// Latest returns the tag of the latest stable release.
func Latest(ctx context.Context, hc *http.Client) (string, error) {
	body, status, err := fetch(ctx, hc, latestURL, 1<<20)
	if err != nil {
		return "", err
	}
	if status == http.StatusNotFound {
		return "", ErrNoRelease
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("github answered HTTP %d for the latest release", status)
	}
	var r struct {
		Tag        string `json:"tag_name"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return "", err
	}
	if r.Draft || r.Prerelease || !Stable(r.Tag) {
		return "", ErrNoRelease
	}
	return r.Tag, nil
}

// ArchiveName is the goreleaser archive for this platform.
func ArchiveName(tag string) string {
	ext := ".tar.gz"
	if runtime.GOOS == "windows" {
		ext = ".zip"
	}
	return fmt.Sprintf("obramat_%s_%s_%s%s", strings.TrimPrefix(tag, "v"), runtime.GOOS, runtime.GOARCH, ext)
}

// Install downloads tag's archive, verifies it, and replaces exe.
func Install(ctx context.Context, hc *http.Client, tag, exe string) error {
	name := ArchiveName(tag)
	sums, status, err := fetch(ctx, hc, downloadURL+tag+"/checksums.txt", 1<<20)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("checksums.txt: HTTP %d", status)
	}
	archive, status, err := fetch(ctx, hc, downloadURL+tag+"/"+name, maxDownload)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("%s: HTTP %d (no build for %s/%s?)", name, status, runtime.GOOS, runtime.GOARCH)
	}
	if err := Verify(archive, sums, name); err != nil {
		return err
	}
	bin, err := Extract(archive, strings.HasSuffix(name, ".zip"))
	if err != nil {
		return err
	}
	return Replace(exe, bin)
}

// Verify checks data against its line in a goreleaser checksums.txt.
func Verify(data, sums []byte, name string) error {
	sum := sha256.Sum256(data)
	for _, line := range strings.Split(string(sums), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name {
			if f[0] != hex.EncodeToString(sum[:]) {
				return fmt.Errorf("checksum mismatch for %s: refusing to install", name)
			}
			return nil
		}
	}
	return fmt.Errorf("checksums.txt has no entry for %s", name)
}

// Extract returns the obramat executable from an archive. Nothing else in
// the archive is read, and no archive path is ever written to disk.
func Extract(archive []byte, isZip bool) ([]byte, error) {
	want := "obramat"
	if isZip {
		want = "obramat.exe"
		zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
		if err != nil {
			return nil, err
		}
		for _, f := range zr.File {
			if f.Name == want && f.Mode().IsRegular() {
				rc, err := f.Open()
				if err != nil {
					return nil, err
				}
				defer rc.Close()
				return io.ReadAll(io.LimitReader(rc, maxDownload))
			}
		}
		return nil, fmt.Errorf("archive has no %s", want)
	}
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil, fmt.Errorf("archive has no %s", want)
		}
		if err != nil {
			return nil, err
		}
		if h.Name == want && h.Typeflag == tar.TypeReg {
			return io.ReadAll(io.LimitReader(tr, maxDownload))
		}
	}
}

// Replace swaps exe for bin through a temp file in the same directory. On
// Windows the running file is renamed aside first, since it cannot be
// overwritten while it runs.
func Replace(exe string, bin []byte) error {
	if len(bin) == 0 {
		return errors.New("empty executable in archive")
	}
	fi, err := os.Stat(exe)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(exe), ".obramat-update-*")
	if err != nil {
		return fmt.Errorf("cannot write next to %s: %w", exe, err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(bin); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(fi.Mode().Perm()); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		old := exe + ".old"
		_ = os.Remove(old)
		if err := os.Rename(exe, old); err != nil {
			return err
		}
	}
	return os.Rename(tmp.Name(), exe)
}
