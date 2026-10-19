package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/n0nuser/obramat-cli/internal/api"
	"github.com/n0nuser/obramat-cli/internal/config"
)

// blockPause is how long the CLI stays away from obramat.es after its bot
// protection refused a request. Retrying sooner was observed to keep the
// block in place (a refused sweep was still refused 45 s later), and every
// refused request makes the address look worse.
const blockPause = 10 * time.Minute

func (a *App) pauseFile() string { return filepath.Join(a.Paths.CacheDir, "blocked-until") }

// recordBlock starts a pause when err is a block.
func (a *App) recordBlock(err error) {
	var e *api.Error
	if a.Paths.CacheDir == "" || !errors.As(err, &e) || e.Kind != api.KindBlocked {
		return
	}
	until := time.Now().Add(blockPause).UTC().Format(time.RFC3339)
	if err := config.WriteAtomic(a.pauseFile(), []byte(until), 0o600); err != nil {
		a.notice("could not record the pause: %v", err)
	}
}

// pausedUntil reports an active pause.
func (a *App) pausedUntil() (time.Time, bool) {
	raw, err := os.ReadFile(a.pauseFile())
	if err != nil {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(string(raw)))
	return t, err == nil && time.Now().Before(t)
}

// needsSite reports whether a command talks to obramat.es. Local commands
// (config, watch list, sinks, skills...) keep working during a pause.
func needsSite(cmd *cobra.Command) bool {
	path := strings.TrimPrefix(cmd.CommandPath(), "obramat ")
	for _, p := range []string{"search", "product", "item", "category", "store", "watch add", "watch check", "watch run", "doctor"} {
		if path == p || strings.HasPrefix(path, p+" ") {
			return true
		}
	}
	return false
}

func (a *App) checkPause(cmd *cobra.Command) error {
	if !needsSite(cmd) {
		return nil
	}
	if until, ok := a.pausedUntil(); ok {
		return &api.Error{Kind: api.KindBlocked, Retryable: true,
			Msg: fmt.Sprintf("paused until %s after obramat.es refused a request; trying sooner tends to extend the block (delete %s to override)", until.Local().Format("15:04"), a.pauseFile())}
	}
	return nil
}
