package cli

import (
	"embed"
	"fmt"
	"io"
	"io/fs"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/spf13/cobra"

	"github.com/n0nuser/obramat-cli/internal/api"
	"github.com/n0nuser/obramat-cli/internal/config"
	"github.com/n0nuser/obramat-cli/internal/output"
	"github.com/n0nuser/obramat-cli/internal/sink"
)

func (a *App) configCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Read and write config.toml",
		Long: `Read and write config.toml. Keys are dotted paths.

Examples:
  obramat config path
  obramat config set store 4
  obramat config set watch.interval 1h
  obramat config set sinks.phone.type ntfy
  obramat config set sinks.script.command '["/home/me/bin/on-event", "--quiet"]'
  obramat config get sinks.phone`,
	}
	cmd.AddCommand(
		&cobra.Command{Use: "path", Short: "Print where the CLI keeps its files", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
			return a.Printer.Print(map[string]string{"config": a.Paths.ConfigFile, "state": a.Paths.StateDB, "cache": a.Paths.CacheDir, "logs": a.Paths.StateDir})
		}},
		&cobra.Command{Use: "list", Short: "Print the whole config", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
			return a.Printer.Print(a.Cfg)
		}},
		&cobra.Command{Use: "get KEY", Short: "Print one key", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			doc, err := a.configDoc()
			if err != nil {
				return err
			}
			var cur any = doc
			for _, part := range strings.Split(args[0], ".") {
				m, ok := cur.(map[string]any)
				if !ok {
					return output.Usagef("config has no key %q", args[0])
				}
				if cur, ok = m[part]; !ok {
					return output.Usagef("config has no key %q", args[0])
				}
			}
			return a.Printer.Print(cur)
		}},
		&cobra.Command{Use: "set KEY VALUE", Short: "Set one key; exec commands take a TOML string array", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
			doc, err := a.configDoc()
			if err != nil {
				return err
			}
			path := strings.Split(args[0], ".")
			value, err := typedValue(path, args[1])
			if err != nil {
				return err
			}
			cur := doc
			for _, part := range path[:len(path)-1] {
				next, ok := cur[part].(map[string]any)
				if !ok {
					next = map[string]any{}
					cur[part] = next
				}
				cur = next
			}
			cur[path[len(path)-1]] = value
			raw, err := toml.Marshal(doc)
			if err != nil {
				return err
			}
			// Decode strictly before writing, so an unknown key never lands.
			dec := toml.NewDecoder(strings.NewReader(string(raw)))
			dec.DisallowUnknownFields()
			var cfg config.Config
			if err := dec.Decode(&cfg); err != nil {
				return output.Usagef("%q is not a valid config key or value: %v", args[0], err)
			}
			if err := config.Save(a.Paths.ConfigFile, cfg); err != nil {
				return err
			}
			return a.Printer.Print(map[string]any{args[0]: value})
		}},
	)
	return cmd
}

func (a *App) configDoc() (map[string]any, error) {
	doc := map[string]any{}
	raw, err := os.ReadFile(a.Paths.ConfigFile)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if err := toml.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	return doc, nil
}

// typedValue reads a config value by what its key needs: an integer store
// code, a string array for an exec command, a string otherwise.
func typedValue(path []string, s string) (any, error) {
	switch {
	case len(path) == 1 && path[0] == "store":
		n, err := strconv.Atoi(s)
		if err != nil || n < 0 {
			return nil, output.Usagef("store is a store code, a number (see `obramat store list`)")
		}
		return int64(n), nil
	case len(path) == 3 && path[0] == "sinks" && path[2] == "command":
		var v struct {
			V []string `toml:"v"`
		}
		dec := toml.NewDecoder(strings.NewReader("v = " + s))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&v); err != nil || len(v.V) == 0 {
			return nil, output.Usagef("%s takes a TOML array of strings, for example '[\"/home/me/bin/on-event\"]'", strings.Join(path, "."))
		}
		return v.V, nil
	}
	return s, nil
}

// listProbe is the search doctor runs to prove the site answers and parses.
var listProbe = api.ListParams{Query: "cemento"}

type check struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

type doctorReport struct {
	OK     bool    `json:"ok"`
	Checks []check `json:"checks"`
}

func (r doctorReport) Pretty(w io.Writer, color bool) {
	rows := make([][]string, 0, len(r.Checks))
	for _, c := range r.Checks {
		mark := "ok"
		if !c.OK {
			mark = "FAIL"
		}
		rows = append(rows, []string{mark, c.Name, c.Detail})
	}
	output.Table(w, color, nil, rows)
}

func (a *App) doctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check config, sinks, the default store, the site and the watch schedule",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			rep := doctorReport{OK: true}
			add := func(name string, err error, detail string) {
				c := check{Name: name, OK: err == nil, Detail: detail}
				if err != nil {
					c.Detail, rep.OK = err.Error(), false
				}
				rep.Checks = append(rep.Checks, c)
			}
			add("config", nil, a.Paths.ConfigFile)
			names := make([]string, 0, len(a.Cfg.Sinks))
			for n := range a.Cfg.Sinks {
				names = append(names, n)
			}
			sort.Strings(names)
			for _, n := range names {
				add("sink "+n, sink.Validate(n, a.Cfg.Sinks[n]), a.Cfg.Sinks[n].Type)
			}
			if l, err := a.Client.List(ctx, listProbe); err != nil {
				add("search", err, "")
			} else {
				add("search", nil, fmt.Sprintf("%d results for %q", l.Total, listProbe.Query))
			}
			if s, err := a.storeContext(ctx, ""); err != nil {
				add("default store", err, "")
			} else if s.ID == 0 {
				add("default store", nil, "none: web prices, no stock (`obramat config set store ID`)")
			} else {
				add("default store", nil, fmt.Sprintf("%d %s", s.ID, s.Name))
			}
			if db, err := a.db(); err != nil {
				add("watch state", err, "")
			} else if ws, err := db.Watches(ctx); err != nil {
				add("watch state", err, "")
			} else {
				add("watch state", nil, fmt.Sprintf("%d watches in %s", len(ws), a.Paths.StateDB))
			}
			platform, files := serviceFiles()
			if files != nil {
				st, _ := serviceState(platform, files)
				add("schedule", nil, platform+": "+st)
			}
			if err := a.Printer.Print(rep); err != nil {
				return err
			}
			if !rep.OK {
				return fmt.Errorf("doctor found problems")
			}
			return nil
		},
	}
}

//go:embed skills/*.md
var skillFiles embed.FS

func (a *App) skillsCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "skills", Short: "Embedded usage notes for agents and people"}
	names := func() []string {
		entries, _ := fs.ReadDir(skillFiles, "skills")
		out := make([]string, 0, len(entries))
		for _, e := range entries {
			out = append(out, strings.TrimSuffix(e.Name(), ".md"))
		}
		return out
	}
	cmd.AddCommand(
		&cobra.Command{Use: "list", Short: "List the notes", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
			return a.Printer.Print(names())
		}},
		&cobra.Command{Use: "get NAME", Short: "Print one note as markdown", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			raw, err := skillFiles.ReadFile("skills/" + args[0] + ".md")
			if err != nil {
				return output.Usagef("no note %q. Available: %s", args[0], strings.Join(names(), ", "))
			}
			_, err = a.Stdout.Write(raw)
			return err
		}},
	)
	return cmd
}
