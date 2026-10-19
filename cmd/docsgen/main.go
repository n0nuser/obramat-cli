// docsgen writes the docs site's command reference from the cobra tree, so
// the published commands and flags are always the binary's. `make docs-check`
// regenerates in CI and fails when the committed pages are stale.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/n0nuser/obramat-cli/internal/cli"
)

const banner = "{/* Written by cmd/docsgen from the command tree; edit the Go help strings instead. */}\n\n"

func main() {
	dir := flag.String("out", "docs-site/content/docs/reference", "output directory")
	flag.Parse()
	root := cli.CommandTree("0.0.0")
	if err := os.MkdirAll(*dir, 0o755); err != nil {
		die(err)
	}
	// Remove what a previous run wrote, so a deleted command loses its page.
	// index.mdx is hand-written and stays.
	entries, err := os.ReadDir(*dir)
	if err != nil {
		die(err)
	}
	for _, e := range entries {
		if n := e.Name(); n != "index.mdx" && (strings.HasSuffix(n, ".mdx") || n == "meta.json") {
			if err := os.Remove(filepath.Join(*dir, n)); err != nil {
				die(err)
			}
		}
	}

	var g strings.Builder
	g.WriteString("---\ntitle: Global flags\ndescription: Flags every command accepts.\n---\n\n" + banner)
	flagTable(&g, root.PersistentFlags())
	write(*dir, "global-flags.mdx", g.String())
	pages := []string{"index", "global-flags"}

	var top []*cobra.Command
	for _, c := range root.Commands() {
		if !c.Hidden && c.Name() != "help" && c.Name() != "completion" {
			top = append(top, c)
		}
	}
	sort.Slice(top, func(i, j int) bool { return top[i].Name() < top[j].Name() })
	for _, c := range top {
		var b strings.Builder
		fmt.Fprintf(&b, "---\ntitle: %s\ndescription: %s\n---\n\n%s", c.Name(), yaml(c.Short), banner)
		walk(&b, c, 2)
		write(*dir, c.Name()+".mdx", b.String())
		pages = append(pages, c.Name())
	}
	meta := fmt.Sprintf("{\n  \"title\": \"Command reference\",\n  \"pages\": [\n    \"%s\"\n  ]\n}\n", strings.Join(pages, "\",\n    \""))
	write(*dir, "meta.json", meta)
	fmt.Fprintf(os.Stderr, "docsgen: %d pages in %s\n", len(pages)-1, *dir)
}

func walk(b *strings.Builder, c *cobra.Command, level int) {
	if c.Runnable() || c.HasSubCommands() {
		fmt.Fprintf(b, "%s `%s`\n\n", strings.Repeat("#", min(level, 4)), c.CommandPath())
		if c.Short != "" {
			b.WriteString(mdx(c.Short) + "\n\n")
		}
		if c.Runnable() {
			fmt.Fprintf(b, "```bash\n%s\n```\n\n", c.UseLine())
		}
		if long := strings.TrimSpace(c.Long); long != "" && long != c.Short {
			body, examples, _ := strings.Cut(long, "Examples:")
			if body = strings.TrimSpace(body); body != "" && body != c.Short {
				b.WriteString(mdx(body) + "\n\n")
			}
			var lines []string
			for _, l := range strings.Split(examples, "\n") {
				if l = strings.TrimSpace(l); l != "" {
					lines = append(lines, l)
				}
			}
			if len(lines) > 0 {
				fmt.Fprintf(b, "```bash\n%s\n```\n\n", strings.Join(lines, "\n"))
			}
		}
		flagTable(b, c.LocalNonPersistentFlags())
	}
	for _, sub := range c.Commands() {
		if !sub.Hidden && sub.Name() != "help" {
			walk(b, sub, level+1)
		}
	}
}

func flagTable(b *strings.Builder, fs *pflag.FlagSet) {
	var rows []string
	fs.VisitAll(func(f *pflag.Flag) {
		if f.Hidden {
			return
		}
		def := "-"
		if f.DefValue != "" && f.DefValue != "[]" {
			def = "`" + f.DefValue + "`"
		}
		rows = append(rows, fmt.Sprintf("| `--%s` | %s | %s | %s |", f.Name, f.Value.Type(), def, cell(f.Usage)))
	})
	if len(rows) == 0 {
		return
	}
	b.WriteString("| Flag | Type | Default | Description |\n| --- | --- | --- | --- |\n" + strings.Join(rows, "\n") + "\n\n")
}

// mdx escapes the two characters MDX treats as syntax in prose.
func mdx(s string) string { return strings.NewReplacer("{", `\{`, "<", `\<`).Replace(s) }

func cell(s string) string { return mdx(strings.ReplaceAll(strings.ReplaceAll(s, "|", `\|`), "\n", " ")) }

func yaml(s string) string { return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"` }

func write(dir, name, body string) {
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		die(err)
	}
}

func die(err error) {
	fmt.Fprintln(os.Stderr, "docsgen:", err)
	os.Exit(1)
}
