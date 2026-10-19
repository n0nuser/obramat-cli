package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// ServerName is the key the server is registered under in every harness.
const ServerName = "obramat"

var Harnesses = []string{"claude-code", "codex", "cursor"}

// Change reports what an install did to which file.
type Change struct {
	Harness string   `json:"harness"`
	Path    string   `json:"path"`
	Action  string   `json:"action"` // added, updated, unchanged
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

func configPath(harness string) (string, error) {
	home, err := os.UserHomeDir()
	switch harness {
	case "claude-code":
		if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
			return filepath.Join(d, ".claude.json"), nil
		}
		return filepath.Join(home, ".claude.json"), err
	case "codex":
		if d := os.Getenv("CODEX_HOME"); d != "" {
			return filepath.Join(d, "config.toml"), nil
		}
		return filepath.Join(home, ".codex", "config.toml"), err
	case "cursor":
		return filepath.Join(home, ".cursor", "mcp.json"), err
	}
	return "", fmt.Errorf("unknown harness %q: use %s", harness, strings.Join(Harnesses, ", "))
}

// Install registers the server in a harness config. Only this server's
// command and args are written; other servers, and other keys of this one,
// are left as they were. A second run reports "unchanged".
func Install(harness, command string, args []string) (Change, error) {
	path, err := configPath(harness)
	if err != nil {
		return Change{}, err
	}
	ch := Change{Harness: harness, Path: path, Command: command, Args: args}
	if harness == "codex" {
		ch.Action, err = installTOML(path, command, args)
	} else {
		ch.Action, err = installJSON(path, command, args)
	}
	return ch, err
}

func toAny(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

func installJSON(path, command string, args []string) (string, error) {
	doc := map[string]any{}
	raw, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	if len(bytes.TrimSpace(raw)) > 0 {
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber() // keep large integers exactly as they were
		if err := dec.Decode(&doc); err != nil || doc == nil {
			return "", fmt.Errorf("%s is not a JSON object; fix it by hand first", path)
		}
		if dec.More() {
			return "", fmt.Errorf("%s has data after its JSON object; fix it by hand first", path)
		}
	}
	servers, ok := doc["mcpServers"].(map[string]any)
	if !ok {
		if doc["mcpServers"] != nil {
			return "", fmt.Errorf("%s: mcpServers is not an object; fix it by hand first", path)
		}
		servers = map[string]any{}
	}
	entry, _ := servers[ServerName].(map[string]any)
	action := "added"
	if entry != nil {
		if entry["command"] == command && reflect.DeepEqual(entry["args"], toAny(args)) {
			return "unchanged", nil
		}
		action = "updated"
	} else {
		entry = map[string]any{}
	}
	entry["command"], entry["args"] = command, toAny(args)
	servers[ServerName] = entry
	doc["mcpServers"] = servers
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", err
	}
	return action, writeKeepingMode(path, append(out, '\n'))
}

// installTOML edits codex's config.toml as text, so the user's comments and
// key order survive: a new table is appended; an existing one written as a
// plain [mcp_servers.obramat] table has its command and args lines replaced.
// Any other spelling of the same table is left for the user to edit.
func installTOML(path, command string, args []string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	var doc map[string]any
	if err := toml.Unmarshal(raw, &doc); err != nil {
		return "", fmt.Errorf("%s is not valid TOML: %w", path, err)
	}
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = strconv.Quote(a)
	}
	cmdLine := "command = " + strconv.Quote(command)
	argsLine := "args = [" + strings.Join(quoted, ", ") + "]"
	header := "[mcp_servers." + ServerName + "]"

	servers, _ := doc["mcp_servers"].(map[string]any)
	entry, exists := servers[ServerName].(map[string]any)
	if exists && entry["command"] == command && reflect.DeepEqual(entry["args"], toAny(args)) {
		return "unchanged", nil
	}
	text := string(raw)
	if !exists {
		if text != "" && !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		if text != "" {
			text += "\n"
		}
		return "added", writeKeepingMode(path, []byte(text+header+"\n"+cmdLine+"\n"+argsLine+"\n"))
	}
	lines := strings.Split(text, "\n")
	in, found := false, false
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "[") {
			in = t == header
			found = found || in
			continue
		}
		if in && strings.HasPrefix(t, "command") && strings.Contains(t, "=") {
			lines[i] = cmdLine
		}
		if in && strings.HasPrefix(t, "args") && strings.Contains(t, "=") && strings.HasSuffix(t, "]") {
			lines[i] = argsLine
		}
	}
	out := strings.Join(lines, "\n")
	var check map[string]any
	if !found || toml.Unmarshal([]byte(out), &check) != nil {
		return "", fmt.Errorf("%s defines mcp_servers.%s in a form this command does not rewrite; set it by hand:\n%s\n%s\n%s", path, ServerName, header, cmdLine, argsLine)
	}
	return "updated", writeKeepingMode(path, []byte(out))
}

// writeKeepingMode replaces the file atomically, through symlinks, keeping
// its mode (0600 for a new file: harness configs hold other servers' secrets).
func writeKeepingMode(path string, data []byte) error {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	mode := os.FileMode(0o600)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
