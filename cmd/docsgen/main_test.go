package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMDXEscapesOpeningDelimitersOnly(t *testing.T) {
	if got := mdx("pass <ref> and {x}"); got != `pass \<ref> and \{x}` {
		t.Fatal(got)
	}
	if got := cell("a|b\nc"); got != `a\|b c` {
		t.Fatal(got)
	}
}

// The generator runs against a temp dir: it writes one page per command, a
// global-flags page and meta.json, keeps the hand-written index, and removes a
// stale page from an earlier run.
func TestMainRegeneratesTheReference(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.mdx"), []byte("# hand written"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "gone.mdx"), []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}

	old := os.Args
	os.Args = []string{"docsgen", "-out", dir}
	t.Cleanup(func() { os.Args = old })
	main()

	for _, name := range []string{"index.mdx", "global-flags.mdx", "meta.json", "search.mdx", "auth.mdx", "notes.txt"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s missing: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "gone.mdx")); !os.IsNotExist(err) {
		t.Error("a stale page survived")
	}
	meta, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(meta), `"search"`) || !strings.Contains(string(meta), `"auth"`) {
		t.Fatalf("meta.json: %s", meta)
	}
	search, err := os.ReadFile(filepath.Join(dir, "search.mdx"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(search), "## `obramat search`") || !strings.Contains(string(search), "| Flag |") {
		t.Fatalf("search page:\n%s", search)
	}
}
