package harvest

import (
	"os"
	"strings"
	"testing"
)

// The port must be one the OS would let us bind, so Chrome can.
func TestFreePort(t *testing.T) {
	port, err := FreePort()
	if err != nil {
		t.Fatal(err)
	}
	if port < 1024 || port > 65535 {
		t.Fatalf("port %d looks wrong", port)
	}
}

// localhost is rewritten to 127.0.0.1: on some systems it resolves to [::1]
// first and Chrome only listens on IPv4.
func TestNormalizeCDPURL(t *testing.T) {
	cases := map[string]string{
		"http://localhost:9222":            "http://127.0.0.1:9222",
		"http://localhost:9222/":           "http://127.0.0.1:9222/",
		"ws://localhost:9222/devtools/x":   "ws://127.0.0.1:9222/devtools/x",
		"http://localhost":                 "http://127.0.0.1",
		"http://127.0.0.1:9222":            "http://127.0.0.1:9222",
		"ws://127.0.0.1:9222/devtools/b/x": "ws://127.0.0.1:9222/devtools/b/x",
		"http://chrome-host:9222":          "http://chrome-host:9222",
	}
	for in, want := range cases {
		if got := NormalizeCDPURL(in); got != want {
			t.Errorf("NormalizeCDPURL(%q) = %q, want %q", in, got, want)
		}
	}
}

// The launch uses a non-default profile (required for the port) and binds the
// endpoint to the loopback address, and it is never headless.
func TestChromeArgs(t *testing.T) {
	args := ChromeArgs("/tmp/profile", 9333)
	joined := strings.Join(args, " ")
	for _, want := range []string{"--user-data-dir=/tmp/profile", "--remote-debugging-port=9333", "--remote-debugging-address=127.0.0.1"} {
		if !strings.Contains(joined, want) {
			t.Errorf("args %v missing %q", args, want)
		}
	}
	if strings.Contains(joined, "headless") {
		t.Errorf("args %v ask for a headless window", args)
	}
}

// Cleanup must remove the temp profile even when no browser was started.
func TestCleanupRemovesTheTempProfile(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(dir+"/Default", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir+"/Default/Cookies", []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	Cleanup(nil, dir)
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("temp profile survived Cleanup: %v", err)
	}
}
