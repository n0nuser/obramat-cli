package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/n0nuser/obramat-cli/internal/api"
	"github.com/n0nuser/obramat-cli/internal/config"
)

// The flag wins over the environment, and either may be empty.
func TestResolveChromeEnv(t *testing.T) {
	t.Setenv(chromeURLEnv, "http://env:9222")
	t.Setenv(chromePathEnv, "/env/chrome")
	if u, c := resolveChromeEnv("", ""); u != "http://env:9222" || c != "/env/chrome" {
		t.Fatalf("env only: %q %q", u, c)
	}
	if u, c := resolveChromeEnv("http://flag:9222", "/flag/chrome"); u != "http://flag:9222" || c != "/flag/chrome" {
		t.Fatalf("flag wins: %q %q", u, c)
	}
	if u, c := resolveChromeEnv("http://flag:9222", ""); u != "http://flag:9222" || c != "/env/chrome" {
		t.Fatalf("mixed: %q %q", u, c)
	}
}

func TestResolveProfile(t *testing.T) {
	t.Setenv(chromeProfileEnv, "/env/profile")
	if got := resolveProfile(""); got != "/env/profile" {
		t.Fatalf("env only: %q", got)
	}
	if got := resolveProfile("/flag/profile"); got != "/flag/profile" {
		t.Fatalf("flag wins: %q", got)
	}
	t.Setenv(chromeProfileEnv, "")
	if got := resolveProfile(""); got != "" {
		t.Fatalf("neither set: %q", got)
	}
}

func TestStoreFromSession(t *testing.T) {
	v := `%7B%22main_store%22%3A%224%22%2C%22stores_name%22%3A%5B%7B%22id%22%3A%224%22%2C%22name%22%3A%22Obramat+Legan%C3%A9s%22%7D%5D%7D`
	s := &api.Session{Cookies: []api.Cookie{
		{Name: "datadome", Value: "x", Domain: ".obramat.es"},
		{Name: "customer_context", Value: v, Domain: ".obramat.es"},
	}}
	ref, ok := storeFromSession(s)
	if !ok || ref.ID != 4 || ref.Name != "Obramat Leganés" {
		t.Fatalf("%+v %v", ref, ok)
	}
	if _, ok := storeFromSession(&api.Session{}); ok {
		t.Fatal("a session without customer_context reported a store")
	}
}

func TestHarvestResultPretty(t *testing.T) {
	ref := api.StoreRef{ID: 22, Name: "Obramat Salamanca"}
	r := harvestResult{
		Path:    "/home/x/.config/obramat-cli/session.json",
		Format:  "chrome",
		Skipped: 3,
		Session: api.Summary{Cookies: 2, Names: []string{"customer_context", "datadome"}, HasDataDome: true, UserAgent: "UA/155"},
		Store:   &ref,
	}
	var buf bytes.Buffer
	r.Pretty(&buf, false)
	out := buf.String()
	for _, want := range []string{"harvested 2 obramat.es cookies (chrome)", "store Obramat Salamanca (22)", "skipped 3", "UA/155"} {
		if !strings.Contains(out, want) {
			t.Errorf("pretty output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "datadome=") {
		t.Errorf("pretty output leaked a value:\n%s", out)
	}
}

// The command must exist under auth and take the documented flags.
func TestHarvestCommandSurface(t *testing.T) {
	found := false
	for _, c := range CommandTree("test").Commands() {
		if c.Name() != "auth" {
			continue
		}
		for _, s := range c.Commands() {
			if s.Name() != "harvest" {
				continue
			}
			found = true
			for _, flag := range []string{"browser-url", "chrome", "profile", "user-agent", "timeout"} {
				if s.Flags().Lookup(flag) == nil {
					t.Errorf("harvest is missing --%s", flag)
				}
			}
		}
	}
	if !found {
		t.Fatal("no `auth harvest` command")
	}
}

// The Chrome profile lives under the data dir; the XDG paths must agree.
func TestChromeDirIsUnderTheDataDir(t *testing.T) {
	isolate(t, "http://127.0.0.1:1")
	p := config.DefaultPaths()
	if !strings.HasSuffix(p.ChromeDir, filepath.Join("obramat-cli", "chrome")) {
		t.Fatalf("chrome dir %q", p.ChromeDir)
	}
	if !strings.HasPrefix(p.ChromeDir, os.Getenv("XDG_DATA_HOME")) {
		t.Fatalf("chrome dir %q is not under the data home", p.ChromeDir)
	}
}
