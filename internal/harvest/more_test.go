package harvest

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestWaitCDP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/json/version" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Write([]byte(`{"Browser":"Chrome"}`))
	}))
	defer srv.Close()
	port := srv.Listener.Addr().(*net.TCPAddr).Port
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := WaitCDP(ctx, port, time.Second); err != nil {
		t.Fatalf("live port: %v", err)
	}

	// A port nothing answers on times out with guidance.
	dead, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	deadPort := dead.Addr().(*net.TCPAddr).Port
	dead.Close()
	if err := WaitCDP(context.Background(), deadPort, 150*time.Millisecond); err == nil ||
		!strings.Contains(err.Error(), "did not open its devtools port") {
		t.Fatalf("dead port: %v", err)
	}
}

func TestStartChrome(t *testing.T) {
	// A binary that does not exist fails at Start.
	if _, err := StartChrome(filepath.Join(t.TempDir(), "missing"), t.TempDir(), 1); err == nil {
		t.Fatal("a missing browser was started")
	}
	if runtime.GOOS == "windows" {
		return
	}
	if _, err := os.Stat("/bin/sleep"); err != nil {
		t.Skip("no /bin/sleep to start")
	}
	cmd, err := StartChrome("/bin/sleep", t.TempDir(), 1)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if cmd.Process == nil || cmd.Process.Pid == 0 {
		t.Fatal("no process was started")
	}
	Cleanup(cmd, "")
}

func TestDefaultProfileDirs(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the order below is the Linux one")
	}
	t.Setenv("XDG_CONFIG_HOME", "/xdg")
	t.Setenv("HOME", "/home/someone")
	got := DefaultProfileDirs()
	want := []string{"/xdg/google-chrome", "/xdg/chromium", "/home/someone/.config/google-chrome", "/home/someone/.config/chromium"}
	if len(got) != len(want) {
		t.Fatalf("%v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}
