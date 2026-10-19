package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/n0nuser/obramat-cli/internal/output"
	"github.com/n0nuser/obramat-cli/internal/updater"
)

func (a *App) updateCmd() *cobra.Command {
	var yes, check bool
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update obramat to the latest release",
		Long: `Check GitHub for the latest release and replace this binary with it, after
verifying the download against the release's checksums.txt. --check only
reports. Development builds (go build, go install from a branch) are not
replaced; reinstall those the way you installed them.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			tag, err := updater.Latest(cmd.Context(), updater.Client())
			if errors.Is(err, updater.ErrNoRelease) {
				return a.Printer.Print(map[string]any{"current_version": a.Version, "latest_version": nil, "update_available": false})
			}
			if err != nil {
				return err
			}
			available := updater.Stable(a.Version) && updater.Newer(tag, a.Version)
			if check || !available {
				return a.Printer.Print(map[string]any{"current_version": a.Version, "latest_version": tag, "update_available": available})
			}
			if !yes {
				if !a.Interactive {
					return output.Usagef("%s is available; run `obramat update --yes`", tag)
				}
				if !a.askUpdate(tag) {
					return nil
				}
			}
			return a.installUpdate(cmd.Context(), tag)
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "update without asking")
	cmd.Flags().BoolVar(&check, "check", false, "only report whether an update exists")
	return cmd
}

func (a *App) askUpdate(tag string) bool {
	fmt.Fprintf(a.Stderr, "obramat %s is available (this is %s). Update? [Y/n] ", strings.TrimPrefix(tag, "v"), a.Version)
	line, err := bufio.NewReader(a.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return false
	}
	ans := strings.ToLower(strings.TrimSpace(line))
	return ans == "" || ans == "y" || ans == "yes"
}

func (a *App) installUpdate(ctx context.Context, tag string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return err
	}
	if err := updater.Install(ctx, updater.Client(), tag, exe); err != nil {
		return err
	}
	a.notice("updated to %s", strings.TrimPrefix(tag, "v"))
	return nil
}

// maybeUpdate checks for a release at most once a day after an interactive
// command succeeded. Scripts, agents and long-running commands never see it.
func (a *App) maybeUpdate(ctx context.Context, cmd *cobra.Command) {
	if cmd == nil || !a.Interactive || !updater.Stable(a.Version) || os.Getenv("OBRAMAT_NO_UPDATE_CHECK") != "" {
		return
	}
	top := cmd
	for top.Parent() != nil && top.Parent().Parent() != nil {
		top = top.Parent()
	}
	switch top.Name() {
	case "update", "mcp", "watch", "completion", "help":
		return
	}
	stamp := filepath.Join(a.Paths.CacheDir, "update-check")
	if fi, err := os.Stat(stamp); err == nil && time.Since(fi.ModTime()) < 24*time.Hour {
		return
	}
	if os.MkdirAll(a.Paths.CacheDir, 0o700) != nil || os.WriteFile(stamp, nil, 0o600) != nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	tag, err := updater.Latest(ctx, updater.Client())
	if err != nil || !updater.Newer(tag, a.Version) {
		return
	}
	if a.askUpdate(tag) {
		if err := a.installUpdate(context.Background(), tag); err != nil {
			a.notice("update failed: %v", err)
		}
	}
}
