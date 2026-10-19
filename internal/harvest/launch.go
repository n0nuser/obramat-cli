package harvest

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"
)

// cdpStartTimeout is how long a launched browser gets to open its devtools
// port before the launch is called a failure.
const cdpStartTimeout = 20 * time.Second

// FreePort asks the OS for a free TCP port on the loopback interface. Chrome
// 136+ only opens --remote-debugging-port for a non-default user-data-dir, so
// the caller passes this port to a browser it starts itself.
func FreePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// ChromeArgs are the flags for a launch from a copied profile: a visible
// window (so a person can clear a challenge) and a devtools port bound to the
// loopback address.
func ChromeArgs(userDataDir string, port int) []string {
	return []string{
		"--user-data-dir=" + userDataDir,
		fmt.Sprintf("--remote-debugging-port=%d", port),
		"--remote-debugging-address=127.0.0.1",
		"--no-first-run",
		"--no-default-browser-check",
		"--start-maximized",
	}
}

// StartChrome launches the browser on the prepared profile. The caller must
// pass the result to Cleanup.
func StartChrome(path, userDataDir string, port int) (*exec.Cmd, error) {
	cmd := exec.Command(path, ChromeArgs(userDataDir, port)...)
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting %s: %w", path, err)
	}
	return cmd, nil
}

// WaitCDP waits until the devtools endpoint answers. 127.0.0.1 is used
// throughout: localhost can resolve to [::1] first, where nothing is bound.
func WaitCDP(ctx context.Context, port int, timeout time.Duration) error {
	url := fmt.Sprintf("http://127.0.0.1:%d", port)
	deadline := time.Now().Add(timeout)
	for {
		if DebugURLReachable(ctx, url, nil) {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("a browser started from the copied profile did not open its devtools port within %s. Close the Chrome using that profile, or the OS keyring is not sharing the cookie key; a fresh window opens without --profile", timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// Cleanup stops a started browser and removes the temp profile. It must always
// run, on every path: the directory holds a copy of the user's cookies.
func Cleanup(cmd *exec.Cmd, dir string) {
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}
	if dir != "" {
		_ = os.RemoveAll(dir)
	}
}

// NormalizeCDPURL rewrites a localhost host to 127.0.0.1, so an endpoint a
// person typed cannot miss over IPv6.
func NormalizeCDPURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(u.Hostname(), "localhost") {
		return raw
	}
	host := "127.0.0.1"
	if p := u.Port(); p != "" {
		host += ":" + p
	}
	u.Host = host
	return u.String()
}
