package cli

import (
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/n0nuser/obramat-cli/internal/output"
)

// The scheduler runs `obramat watch check` on a timer; there is no daemon.
// systemd user units on Linux, a launchd agent on macOS, and a printed
// schtasks command on Windows.

const unitName = "obramat-watch"

type serviceView struct {
	Platform string   `json:"platform"`
	Unit     string   `json:"unit,omitempty"`
	Files    []string `json:"files,omitempty"`
	State    string   `json:"state,omitempty"`
	LastRun  string   `json:"last_run,omitempty"`
	Log      string   `json:"log,omitempty"`
	Command  string   `json:"command,omitempty"`
	Hint     string   `json:"hint,omitempty"`
}

func (v serviceView) Pretty(w io.Writer, color bool) {
	rows := [][]string{{"platform", v.Platform}}
	add := func(k, val string) {
		if val != "" {
			rows = append(rows, []string{k, val})
		}
	}
	add("unit", v.Unit)
	for _, f := range v.Files {
		add("file", f)
	}
	add("state", v.State)
	add("last run", v.LastRun)
	add("log", v.Log)
	add("command", v.Command)
	output.Table(w, color, nil, rows)
	if v.Hint != "" {
		fmt.Fprintln(w, v.Hint)
	}
}

func (a *App) serviceLog() string { return filepath.Join(a.Paths.StateDir, unitName+".log") }

// serviceFiles are the unit files for this platform; nil on Windows.
func serviceFiles() (platform string, files []string) {
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "linux":
		dir := filepath.Join(home, ".config", "systemd", "user")
		return "systemd", []string{filepath.Join(dir, unitName+".service"), filepath.Join(dir, unitName+".timer")}
	case "darwin":
		return "launchd", []string{filepath.Join(home, "Library", "LaunchAgents", "es.n0nuser.obramat-watch.plist")}
	case "windows":
		return "schtasks", nil
	}
	return runtime.GOOS, nil
}

// serviceEnv pins the variables that decide which config and state the CLI
// reads: a scheduler starts jobs with an almost empty environment, and a
// timer reading another state database would check nothing, silently.
func serviceEnv() [][2]string {
	var env [][2]string
	for _, k := range []string{"HOME", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME", "OBRAMAT_CONFIG", "OBRAMAT_STORE"} {
		v := os.Getenv(k)
		if v == "" {
			continue
		}
		if k == "OBRAMAT_CONFIG" {
			if abs, err := filepath.Abs(v); err == nil {
				v = abs
			}
		} else if k != "OBRAMAT_STORE" && !filepath.IsAbs(v) {
			continue
		}
		env = append(env, [2]string{k, v})
	}
	return env
}

func checkArgs() []string { return []string{"watch", "check", "--format", "jsonl"} }

// systemdEscape protects a value from systemd's % specifiers and word
// splitting.
func systemdEscape(s string) string {
	s = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "%", "%%").Replace(s)
	return `"` + s + `"`
}

func systemdUnits(exe, logFile string, iv time.Duration, env [][2]string) (service, timer string) {
	var b strings.Builder
	b.WriteString("[Unit]\nDescription=obramat-cli watch check\n\n[Service]\nType=oneshot\n")
	for _, kv := range env {
		b.WriteString("Environment=" + systemdEscape(kv[0]+"="+kv[1]) + "\n")
	}
	b.WriteString("ExecStart=" + systemdEscape(exe) + " " + strings.Join(checkArgs(), " ") + "\n")
	logPath := strings.ReplaceAll(logFile, "%", "%%")
	b.WriteString("StandardOutput=append:" + logPath + "\nStandardError=append:" + logPath + "\n")
	timer = fmt.Sprintf("[Unit]\nDescription=obramat-cli watch timer\n\n[Timer]\nOnBootSec=5min\nOnUnitActiveSec=%ds\n\n[Install]\nWantedBy=timers.target\n", int(iv.Seconds()))
	return b.String(), timer
}

func xmlText(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;").Replace(s)
}

func launchdPlist(exe, logFile string, iv time.Duration, env [][2]string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>es.n0nuser.obramat-watch</string>
  <key>ProgramArguments</key>
  <array>
`)
	for _, arg := range append([]string{exe}, checkArgs()...) {
		b.WriteString("    <string>" + xmlText(arg) + "</string>\n")
	}
	b.WriteString("  </array>\n")
	if len(env) > 0 {
		b.WriteString("  <key>EnvironmentVariables</key>\n  <dict>\n")
		for _, kv := range env {
			b.WriteString("    <key>" + xmlText(kv[0]) + "</key><string>" + xmlText(kv[1]) + "</string>\n")
		}
		b.WriteString("  </dict>\n")
	}
	fmt.Fprintf(&b, "  <key>StartInterval</key><integer>%d</integer>\n  <key>RunAtLoad</key><true/>\n", int(iv.Seconds()))
	fmt.Fprintf(&b, "  <key>StandardOutPath</key><string>%s</string>\n  <key>StandardErrorPath</key><string>%s</string>\n</dict>\n</plist>\n", xmlText(logFile), xmlText(logFile))
	return b.String()
}

func schtasksLine(exe string, iv time.Duration) string {
	minutes := max(1, int(math.Ceil(iv.Minutes())))
	return fmt.Sprintf(`schtasks /Create /SC MINUTE /MO %d /TN "%s" /TR "\"%s\" %s"`, minutes, unitName, exe, strings.Join(checkArgs(), " "))
}

func (a *App) watchServiceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "service",
		Short: "Run `watch check` on a timer (systemd, launchd, or a printed schtasks line)",
	}
	var iv time.Duration
	install := &cobra.Command{
		Use: "install", Short: "Install and start the timer", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			interval, err := a.interval(iv)
			if err != nil {
				return err
			}
			exe, err := os.Executable()
			if err != nil {
				return err
			}
			platform, files := serviceFiles()
			if platform == "schtasks" {
				return a.Printer.Print(serviceView{Platform: platform, Command: schtasksLine(exe, interval), Hint: "run this in cmd.exe; the task inherits your user's environment"})
			}
			if files == nil {
				return output.Usagef("no scheduler integration on %s; run `obramat watch check` from cron", platform)
			}
			logFile := a.serviceLog()
			for _, p := range append([]string{exe, logFile}, files...) {
				if !filepath.IsAbs(p) {
					return output.Usagef("%s is not an absolute path; set HOME to an absolute path", p)
				}
			}
			if err := os.MkdirAll(a.Paths.StateDir, 0o700); err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(files[0]), 0o755); err != nil {
				return err
			}
			view := serviceView{Platform: platform, Files: files, Log: logFile}
			if platform == "systemd" {
				service, timer := systemdUnits(exe, logFile, interval, serviceEnv())
				if err := os.WriteFile(files[0], []byte(service), 0o644); err != nil {
					return err
				}
				if err := os.WriteFile(files[1], []byte(timer), 0o644); err != nil {
					return err
				}
				for _, c := range [][]string{{"systemctl", "--user", "daemon-reload"}, {"systemctl", "--user", "enable", "--now", unitName + ".timer"}} {
					if out, err := exec.Command(c[0], c[1:]...).CombinedOutput(); err != nil {
						return fmt.Errorf("%s: %v: %s", strings.Join(c, " "), err, strings.TrimSpace(string(out)))
					}
				}
				view.Unit, view.State = unitName+".timer", "enabled"
				view.Hint = "the timer stops at logout unless lingering is on: `loginctl enable-linger`"
			} else {
				if err := os.WriteFile(files[0], []byte(launchdPlist(exe, logFile, interval, serviceEnv())), 0o644); err != nil {
					return err
				}
				_ = exec.Command("launchctl", "unload", files[0]).Run()
				if out, err := exec.Command("launchctl", "load", files[0]).CombinedOutput(); err != nil {
					return fmt.Errorf("launchctl load: %v: %s", err, strings.TrimSpace(string(out)))
				}
				view.Unit, view.State = "es.n0nuser.obramat-watch", "loaded"
			}
			return a.Printer.Print(view)
		},
	}
	install.Flags().DurationVar(&iv, "interval", 0, "timer period (default: config watch.interval, else 30m)")

	uninstall := &cobra.Command{
		Use: "uninstall", Short: "Stop and remove the timer and its log", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			platform, files := serviceFiles()
			if platform == "schtasks" {
				return a.Printer.Print(serviceView{Platform: platform, Command: fmt.Sprintf(`schtasks /Delete /TN "%s" /F`, unitName)})
			}
			if files == nil {
				return output.Usagef("no scheduler integration on %s", platform)
			}
			if installed(files) {
				stop := exec.Command("systemctl", "--user", "disable", "--now", unitName+".timer")
				if platform == "launchd" {
					stop = exec.Command("launchctl", "unload", files[0])
				}
				if out, err := stop.CombinedOutput(); err != nil {
					return fmt.Errorf("%s: %v: %s", strings.Join(stop.Args, " "), err, strings.TrimSpace(string(out)))
				}
			}
			for _, f := range files {
				_ = os.Remove(f)
			}
			if platform == "systemd" {
				_ = exec.Command("systemctl", "--user", "daemon-reload").Run()
			}
			_ = os.Remove(a.serviceLog())
			_ = os.Remove(a.serviceLog() + ".1")
			return a.Printer.Print(serviceView{Platform: platform, State: "removed"})
		},
	}

	status := &cobra.Command{
		Use: "status", Short: "Show whether the timer is installed and running", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			platform, files := serviceFiles()
			if platform == "schtasks" {
				return a.Printer.Print(serviceView{Platform: platform, Command: fmt.Sprintf(`schtasks /Query /TN "%s"`, unitName)})
			}
			st, last := serviceState(platform, files)
			return a.Printer.Print(serviceView{Platform: platform, Files: files, State: st, LastRun: last, Log: a.serviceLog()})
		},
	}
	cmd.AddCommand(install, uninstall, status)
	return cmd
}

func installed(files []string) bool {
	if len(files) == 0 {
		return false
	}
	for _, f := range files {
		if _, err := os.Stat(f); err != nil {
			return false
		}
	}
	return true
}

func serviceState(platform string, files []string) (state, lastRun string) {
	if !installed(files) {
		return "not installed", ""
	}
	if platform == "launchd" {
		if exec.Command("launchctl", "list", "es.n0nuser.obramat-watch").Run() != nil {
			return "not loaded", ""
		}
		return "loaded", ""
	}
	out, _ := exec.Command("systemctl", "--user", "is-active", unitName+".timer").Output()
	state = strings.TrimSpace(string(out))
	if state == "" {
		state = "unknown"
	}
	if last, err := exec.Command("systemctl", "--user", "show", "-p", "LastTriggerUSec", "--value", unitName+".timer").Output(); err == nil {
		if v := strings.TrimSpace(string(last)); v != "" && v != "n/a" {
			lastRun = v
		}
	}
	return state, lastRun
}

// rotateServiceLog keeps the scheduler log to two files of about 1 MiB.
func (a *App) rotateServiceLog() {
	log := a.serviceLog()
	if fi, err := os.Stat(log); err == nil && fi.Size() > 1<<20 {
		_ = os.Rename(log, log+".1")
	}
}
