package cli

import (
	"encoding/json"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/n0nuser/obramat-cli/internal/api"
	"github.com/n0nuser/obramat-cli/internal/output"
	"github.com/n0nuser/obramat-cli/internal/sink"
	"github.com/n0nuser/obramat-cli/internal/state"
)

type sinkRow struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Target  string `json:"target"`
	Problem string `json:"problem,omitempty"`
}

type sinkList []sinkRow

func (l sinkList) Pretty(w io.Writer, color bool) {
	rows := make([][]string, 0, len(l))
	for _, s := range l {
		rows = append(rows, []string{s.Name, s.Type, output.Truncate(s.Target, 50), s.Problem})
	}
	output.Table(w, color, []string{"SINK", "TYPE", "TARGET", "PROBLEM"}, rows)
}

func (a *App) sinkCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sink",
		Short: "Notification targets for watch events, from config.toml",
		Long: `Sinks deliver watch events somewhere besides stdout: an ntfy topic, a JSON
webhook (raw event, or a discord/slack text message), or a local program that
gets the event JSON on stdin. Define them in config.toml.

Examples:
  obramat config set sinks.phone.type ntfy
  obramat config set sinks.phone.url https://ntfy.sh
  obramat config set sinks.phone.topic my-obramat-alerts
  obramat sink test phone`,
	}
	cmd.AddCommand(
		&cobra.Command{
			Use: "list", Short: "List sinks and what is wrong with them", Args: cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				out := sinkList{}
				for name, s := range a.Cfg.Sinks {
					row := sinkRow{Name: name, Type: s.Type, Target: s.URL}
					switch s.Type {
					case "ntfy":
						row.Target = strings.TrimRight(s.URL, "/") + "/" + s.Topic
					case "exec":
						row.Target = strings.Join(s.Command, " ")
					}
					if err := sink.Validate(name, s); err != nil {
						row.Problem = err.Error()
					}
					out = append(out, row)
				}
				sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
				return a.Printer.Print(out)
			},
		},
		&cobra.Command{
			Use: "test NAME", Short: "Send a made-up event to a sink", Args: cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				s, ok := a.Cfg.Sinks[args[0]]
				if !ok {
					return output.Usagef("no sink %q in config", args[0])
				}
				p, _ := json.Marshal(api.Product{Ref: "00000000", Title: "obramat-cli sink test", URL: output.Repo,
					Price: &api.Price{Amount: 9.99, Tax: "IVA", Currency: "EUR"}})
				ev := state.Event{Type: state.ProductNew, Watch: "sink-test", At: time.Now().UTC(), Product: p}
				if err := sink.Deliver(cmd.Context(), args[0], s, ev); err != nil {
					return err
				}
				return a.Printer.Print(map[string]any{"sink": args[0], "delivered": true})
			},
		},
	)
	return cmd
}
