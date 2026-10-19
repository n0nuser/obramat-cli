package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/n0nuser/obramat-cli/internal/mcp"
	"github.com/n0nuser/obramat-cli/internal/output"
)

func (a *App) mcpCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Serve the read commands to an agent harness over MCP (stdio)",
		Long: `Run a Model Context Protocol server on stdin and stdout. Each tool runs the
command it is named after and returns the JSON that command prints; a failed
command returns its error envelope with the exit code.

Tools: ` + strings.Join(mcp.Names(), ", ") + `

Examples:
  obramat mcp install claude-code
  obramat mcp            # normally started by the harness`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			srv := &mcp.Server{Version: a.Version, Run: a.runInProcess}
			return srv.Serve(cmd.Context(), a.Stdin, a.Stdout)
		},
	}
	cmd.AddCommand(&cobra.Command{
		Use:       "install " + strings.Join(mcp.Harnesses, "|"),
		Short:     "Register this binary as an MCP server in a harness config",
		Long:      "Write the server entry into ~/.claude.json (claude-code), ~/.codex/config.toml (codex) or ~/.cursor/mcp.json (cursor). Other entries are kept; a second run says \"unchanged\".",
		Args:      cobra.ExactArgs(1),
		ValidArgs: mcp.Harnesses,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !slices.Contains(mcp.Harnesses, args[0]) {
				return output.Usagef("unknown harness %q: use %s", args[0], strings.Join(mcp.Harnesses, ", "))
			}
			exe, err := os.Executable()
			if err != nil {
				exe = "obramat"
			}
			ch, err := mcp.Install(args[0], exe, []string{"mcp"})
			if err != nil {
				return err
			}
			return a.Printer.Print(installView(ch))
		},
	})
	return cmd
}

type installView mcp.Change

func (v installView) Pretty(w io.Writer, color bool) {
	fmt.Fprintf(w, "%s %s\n%s\n", v.Action, v.Path, output.Dim(v.Command+" "+strings.Join(v.Args, " "), color))
}

// runInProcess runs one CLI command line in a fresh App, so config and watch
// state are re-read on every call, and returns what it printed.
func (a *App) runInProcess(ctx context.Context, argv []string) mcp.Result {
	var out bytes.Buffer
	sub := &App{Version: a.Version, Stdin: strings.NewReader(""), Stdout: &out, Stderr: a.Stderr}
	defer sub.close()
	root := sub.rootCmd()
	root.SetArgs(append([]string{"--no-input"}, argv...))
	root.SetIn(sub.Stdin)
	root.SetOut(&out)
	root.SetErr(a.Stderr)
	cmd, err := root.ExecuteContextC(ctx)
	if err == nil {
		return mcp.Result{Stdout: out.String()}
	}
	sub.recordBlock(err)
	path := "obramat"
	if cmd != nil {
		path = cmd.CommandPath()
	}
	var env bytes.Buffer
	output.PrintError(&env, err, true, a.Version, path)
	return mcp.Result{Stdout: out.String(), ErrorJSON: env.String()}
}
