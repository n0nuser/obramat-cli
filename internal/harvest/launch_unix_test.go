//go:build !windows

package harvest

import (
	"os"
	"os/exec"
	"syscall"
	"testing"
)

// Cleanup must stop the browser it started, not only delete its profile.
func TestCleanupStopsTheBrowser(t *testing.T) {
	cmd := exec.Command("sleep", "300")
	if err := cmd.Start(); err != nil {
		t.Skipf("no sleep to run: %v", err)
	}
	dir := t.TempDir()
	Cleanup(cmd, dir)

	if err := cmd.Process.Signal(syscall.Signal(0)); err == nil {
		t.Fatal("the browser process is still alive after Cleanup")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("temp profile survived Cleanup: %v", err)
	}
}
