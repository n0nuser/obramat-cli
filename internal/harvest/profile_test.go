package harvest

import (
	"os"
	"path/filepath"
	"testing"
)

// touch writes a file at dir/rel, creating parents.
func touch(t *testing.T, dir, rel string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(rel), 0o600); err != nil {
		t.Fatal(err)
	}
}

// isFile is the real one from the package: a path that exists and is not a
// directory.

// The first directory with a cookie store wins, in the order given; the
// override wins over all of them and must itself hold a store.
func TestFindProfileOrder(t *testing.T) {
	chrome, chromium := t.TempDir(), t.TempDir()
	touch(t, chromium, filepath.Join("Default", "Cookies"))

	if got, err := FindProfile("", []string{chrome, chromium}, isFile); err != nil || got != chromium {
		t.Fatalf("only chromium has cookies: %q %v", got, err)
	}
	touch(t, chrome, filepath.Join("Default", "Cookies"))
	if got, err := FindProfile("", []string{chrome, chromium}, isFile); err != nil || got != chrome {
		t.Fatalf("google-chrome comes first: %q %v", got, err)
	}
	// An override wins even when the default list has one.
	if got, err := FindProfile(chromium, []string{chrome}, isFile); err != nil || got != chromium {
		t.Fatalf("override: %q %v", got, err)
	}
	if _, err := FindProfile(t.TempDir(), []string{chrome}, isFile); err == nil {
		t.Fatal("an override with no cookie store was accepted")
	}
	if _, err := FindProfile("", []string{chrome}, func(string) bool { return false }); err == nil {
		t.Fatal("no profile at all was accepted")
	}
}

// A Chromium profile that keeps cookies under Default/Network/Cookies counts.
func TestFindProfileNetworkCookies(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, filepath.Join("Default", "Network", "Cookies"))
	if got, err := FindProfile("", []string{dir}, isFile); err != nil || got != dir {
		t.Fatalf("%q %v", got, err)
	}
}

// Only the cookie store, its journal and the key reference are copied — never
// history, logins or other sites' storage.
func TestCopyPlanOnlyTheCookieStore(t *testing.T) {
	profile := t.TempDir()
	touch(t, profile, "Local State")
	touch(t, profile, filepath.Join("Default", "Preferences"))
	touch(t, profile, filepath.Join("Default", "Cookies"))
	touch(t, profile, filepath.Join("Default", "Cookies-journal"))
	touch(t, profile, filepath.Join("Default", "Network", "Cookies"))
	touch(t, profile, filepath.Join("Default", "Network", "Cookies-wal"))
	touch(t, profile, filepath.Join("Default", "History"))
	touch(t, profile, filepath.Join("Default", "Login Data"))
	touch(t, profile, filepath.Join("Default", "Local Storage", "leveldb", "x.log"))

	plan, err := CopyPlan(profile)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"Local State":                                      true,
		filepath.Join("Default", "Preferences"):            true,
		filepath.Join("Default", "Cookies"):                true,
		filepath.Join("Default", "Cookies-journal"):        true,
		filepath.Join("Default", "Network", "Cookies"):     true,
		filepath.Join("Default", "Network", "Cookies-wal"): true,
	}
	if len(plan) != len(want) {
		t.Fatalf("plan = %v, want %d entries", plan, len(want))
	}
	for _, rel := range plan {
		if !want[rel] {
			t.Errorf("the plan copies %q, which is not a cookie store or key reference", rel)
		}
	}
	for _, bad := range []string{"Default/History", "Default/Login Data", "Default/Local Storage/leveldb/x.log"} {
		for _, rel := range plan {
			if filepath.ToSlash(rel) == bad {
				t.Errorf("the plan copies %q", bad)
			}
		}
	}
}

func TestCopyPlanErrorsWithoutAStore(t *testing.T) {
	profile := t.TempDir()
	touch(t, profile, "Local State")
	if _, err := CopyPlan(profile); err == nil {
		t.Fatal("a profile with no cookie store planned a copy")
	}
}

// The copy lands in a 0700 directory with 0600 files and the same bytes.
func TestCopyProfileFilesIsPrivateAndExact(t *testing.T) {
	profile, dst := t.TempDir(), filepath.Join(t.TempDir(), "temp-profile")
	touch(t, profile, "Local State")
	touch(t, profile, filepath.Join("Default", "Cookies"))
	plan, err := CopyPlan(profile)
	if err != nil {
		t.Fatal(err)
	}
	if err := CopyProfileFiles(profile, dst, plan); err != nil {
		t.Fatal(err)
	}
	di, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if di.Mode().Perm() != 0o700 {
		t.Fatalf("temp profile mode %v, want 0700", di.Mode().Perm())
	}
	for _, rel := range plan {
		src, err := os.ReadFile(filepath.Join(profile, rel))
		if err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(filepath.Join(dst, rel))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(src) {
			t.Errorf("%s differs", rel)
		}
		fi, err := os.Stat(filepath.Join(dst, rel))
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != 0o600 {
			t.Errorf("%s mode %v, want 0600", rel, fi.Mode().Perm())
		}
	}
}
