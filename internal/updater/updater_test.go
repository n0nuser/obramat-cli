package updater

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type rtFunc func(*http.Request) (*http.Response, error)

func (f rtFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// clientWith answers every request from f, so the hardcoded GitHub URLs never
// leave the process.
func clientWith(f func(*http.Request) (int, string)) *http.Client {
	return &http.Client{Transport: rtFunc(func(req *http.Request) (*http.Response, error) {
		status, body := f(req)
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}, Request: req}, nil
	})}
}

func tarGz(t *testing.T, name string, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func zipArchive(t *testing.T, name string, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func checksums(data []byte, name string) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]) + "  " + name + "\n"
}

func TestStable(t *testing.T) {
	for v, want := range map[string]bool{
		"v1.2.3": true, "1.2.3": true, "v0.0.0": true,
		"1.2": false, "v1.2.3-rc1": false, "dev": false, "(devel)": false,
	} {
		if got := Stable(v); got != want {
			t.Errorf("Stable(%q) = %v", v, got)
		}
	}
}

func TestNewer(t *testing.T) {
	cases := []struct {
		latest, current string
		want            bool
	}{
		{"v1.2.4", "v1.2.3", true},
		{"v1.3.0", "v1.2.9", true},
		{"v2.0.0", "v1.9.9", true},
		{"v1.2.3", "v1.2.3", false},
		{"v1.2.2", "v1.2.3", false},
		{"garbage", "v1.2.3", false},
		{"v1.2.3", "dev", false},
	}
	for _, c := range cases {
		if got := Newer(c.latest, c.current); got != c.want {
			t.Errorf("Newer(%q, %q) = %v", c.latest, c.current, got)
		}
	}
}

func TestArchiveName(t *testing.T) {
	ext := ".tar.gz"
	if runtime.GOOS == "windows" {
		ext = ".zip"
	}
	want := fmt.Sprintf("obramat_1.2.3_%s_%s%s", runtime.GOOS, runtime.GOARCH, ext)
	if got := ArchiveName("v1.2.3"); got != want {
		t.Fatalf("ArchiveName = %q, want %q", got, want)
	}
}

func TestLatest(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    string
		want    string
		wantErr error
	}{
		{"stable", 200, `{"tag_name":"v9.9.9"}`, "v9.9.9", nil},
		{"no release", 404, `{}`, "", ErrNoRelease},
		{"server error", 500, `{}`, "", nil}, // any error, checked below
		{"bad json", 200, `{`, "", nil},
		{"draft", 200, `{"tag_name":"v9.9.9","draft":true}`, "", ErrNoRelease},
		{"prerelease", 200, `{"tag_name":"v9.9.9","prerelease":true}`, "", ErrNoRelease},
		{"not semver", 200, `{"tag_name":"nightly"}`, "", ErrNoRelease},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Latest(context.Background(), clientWith(func(*http.Request) (int, string) { return c.status, c.body }))
			if c.wantErr != nil {
				if !errors.Is(err, c.wantErr) {
					t.Fatalf("err %v", err)
				}
				return
			}
			if c.want == "" {
				if err == nil {
					t.Fatal("expected an error")
				}
				return
			}
			if err != nil || got != c.want {
				t.Fatalf("%q %v", got, err)
			}
		})
	}
}

func TestFetchRejectsOversizedBodies(t *testing.T) {
	big := strings.Repeat("x", 32)
	hc := clientWith(func(*http.Request) (int, string) { return 200, big })
	if _, _, err := fetch(context.Background(), hc, "http://example.invalid/x", 8); err == nil {
		t.Fatal("an oversized body was accepted")
	}
}

func TestVerify(t *testing.T) {
	data := []byte("binary")
	sums := []byte(checksums(data, "a.tar.gz") + checksums([]byte("other"), "b.tar.gz"))
	if err := Verify(data, sums, "a.tar.gz"); err != nil {
		t.Fatal(err)
	}
	if err := Verify(data, sums, "b.tar.gz"); err == nil {
		t.Fatal("a wrong checksum was accepted")
	}
	if err := Verify(data, sums, "c.tar.gz"); err == nil {
		t.Fatal("a missing entry was accepted")
	}
}

func TestExtract(t *testing.T) {
	if got, err := Extract(tarGz(t, "obramat", []byte("bin")), false); err != nil || string(got) != "bin" {
		t.Fatalf("tar: %q %v", got, err)
	}
	if _, err := Extract(tarGz(t, "README", []byte("x")), false); err == nil {
		t.Fatal("a tar without the binary was accepted")
	}
	if _, err := Extract([]byte("not gzip"), false); err == nil {
		t.Fatal("broken gzip was accepted")
	}
	if got, err := Extract(zipArchive(t, "obramat.exe", []byte("bin")), true); err != nil || string(got) != "bin" {
		t.Fatalf("zip: %q %v", got, err)
	}
	if _, err := Extract(zipArchive(t, "other.exe", []byte("x")), true); err == nil {
		t.Fatal("a zip without the executable was accepted")
	}
	if _, err := Extract([]byte("not a zip"), true); err == nil {
		t.Fatal("broken zip was accepted")
	}
}

func TestReplace(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "obramat")
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Replace(exe, []byte("new")); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(exe)
	if err != nil || string(got) != "new" {
		t.Fatalf("%q %v", got, err)
	}
	fi, _ := os.Stat(exe)
	if fi.Mode().Perm() != 0o755 {
		t.Fatalf("mode %v", fi.Mode().Perm())
	}
	if err := Replace(exe, nil); err == nil {
		t.Fatal("an empty executable was accepted")
	}
	if err := Replace(filepath.Join(dir, "missing"), []byte("x")); err == nil {
		t.Fatal("a missing target was accepted")
	}
}

// Install exercises the whole path: checksums, archive, verification, extract,
// replace in a temp dir.
func TestInstall(t *testing.T) {
	bin := []byte("#!/bin/sh\necho obramat\n")
	archive := tarGz(t, "obramat", bin)
	name := ArchiveName("v1.2.3")
	sums := checksums(archive, name)

	hc := clientWith(func(req *http.Request) (int, string) {
		if strings.HasSuffix(req.URL.Path, "/checksums.txt") {
			return 200, sums
		}
		return 200, string(archive)
	})
	dir := t.TempDir()
	exe := filepath.Join(dir, "obramat")
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Install(context.Background(), hc, "v1.2.3", exe); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(exe)
	if err != nil || !bytes.Equal(got, bin) {
		t.Fatalf("installed %q %v", got, err)
	}
}

func TestInstallFailures(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "obramat")
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	bin := []byte("new")
	archive := tarGz(t, "obramat", bin)
	name := ArchiveName("v1.2.3")

	cases := []struct {
		name string
		hc   *http.Client
	}{
		{"checksums 404", clientWith(func(*http.Request) (int, string) { return 404, "" })},
		{"archive 404", clientWith(func(req *http.Request) (int, string) {
			if strings.HasSuffix(req.URL.Path, "/checksums.txt") {
				return 200, checksums(archive, name)
			}
			return 404, ""
		})},
		{"checksum mismatch", clientWith(func(req *http.Request) (int, string) {
			if strings.HasSuffix(req.URL.Path, "/checksums.txt") {
				return 200, checksums([]byte("other"), name)
			}
			return 200, string(archive)
		})},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := Install(context.Background(), c.hc, "v1.2.3", exe); err == nil {
				t.Fatal("install succeeded")
			}
			if got, _ := os.ReadFile(exe); string(got) != "old" {
				t.Fatalf("the executable changed: %q", got)
			}
		})
	}
}
