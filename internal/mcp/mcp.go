// Package mcp serves the CLI's read commands to agent harnesses over the
// Model Context Protocol (stdio: one JSON-RPC 2.0 message per line).
//
// A tool is a recipe for a CLI command line, not a second path into the api
// package: what an agent receives is exactly what the command prints.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

// Result is what one CLI run printed: stdout, and the --error-format json
// envelope when it failed.
type Result struct {
	Stdout    string
	ErrorJSON string
}

type Runner func(ctx context.Context, argv []string) Result

type Server struct {
	Version string
	Run     Runner
}

var protocolVersions = []string{"2024-11-05", "2025-03-26", "2025-06-18"}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// Serve answers requests until in closes or ctx ends.
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	lines := make(chan []byte)
	done := make(chan error, 1)
	go func() {
		sc := bufio.NewScanner(in)
		sc.Buffer(make([]byte, 64<<10), 4<<20)
		for sc.Scan() {
			select {
			case lines <- append([]byte(nil), sc.Bytes()...):
			case <-ctx.Done():
				return
			}
		}
		done <- sc.Err()
	}()
	enc := json.NewEncoder(out)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-done:
			return err
		case line := <-lines:
			if strings.TrimSpace(string(line)) == "" {
				continue
			}
			if resp, ok := s.handle(ctx, line); ok {
				if err := enc.Encode(resp); err != nil {
					return err
				}
			}
		}
	}
}

func (s *Server) handle(ctx context.Context, line []byte) (rpcResponse, bool) {
	var req rpcRequest
	if err := json.Unmarshal(line, &req); err != nil {
		return rpcResponse{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{-32700, "invalid json"}}, true
	}
	if len(req.ID) == 0 {
		return rpcResponse{}, false // a notification gets no answer
	}
	reply := func(result any, err *rpcError) (rpcResponse, bool) {
		return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result, Error: err}, true
	}
	if req.JSONRPC != "2.0" {
		return reply(nil, &rpcError{-32600, `"jsonrpc" must be "2.0"`})
	}
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &p)
		version := protocolVersions[len(protocolVersions)-1]
		for _, v := range protocolVersions {
			if v == p.ProtocolVersion {
				version = v
			}
		}
		return reply(map[string]any{
			"protocolVersion": version,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "obramat", "version": s.Version},
			"instructions": "Read-only access to obramat.es through the obramat CLI: search, products, stores and stock per store. " +
				"Tool output is the JSON the matching command prints. Prices and stock depend on the store argument.",
		}, nil)
	case "ping":
		return reply(map[string]any{}, nil)
	case "tools/list":
		return reply(map[string]any{"tools": Tools}, nil)
	case "tools/call":
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return reply(nil, &rpcError{-32602, "invalid params"})
		}
		t, ok := lookup(p.Name)
		if !ok {
			return reply(nil, &rpcError{-32602, fmt.Sprintf("unknown tool %q. Available: %s", p.Name, strings.Join(Names(), ", "))})
		}
		argv, err := t.Argv(p.Arguments)
		if err != nil {
			return reply(nil, &rpcError{-32602, err.Error()})
		}
		res := s.Run(ctx, argv)
		content := []map[string]string{}
		if out := strings.TrimRight(res.Stdout, "\n"); out != "" {
			content = append(content, map[string]string{"type": "text", "text": out})
		}
		if res.ErrorJSON != "" {
			content = append(content, map[string]string{"type": "text", "text": strings.TrimRight(res.ErrorJSON, "\n")})
		}
		return reply(map[string]any{"content": content, "isError": res.ErrorJSON != ""}, nil)
	}
	return reply(nil, &rpcError{-32601, "unknown method " + req.Method})
}

// Param is one tool argument and the CLI flag it becomes. An empty Flag
// makes it a positional argument.
type Param struct {
	Type        string   `json:"type"`
	Description string   `json:"description"`
	Enum        []string `json:"enum,omitempty"`
	Items       *Param   `json:"items,omitempty"`
	flag        string
}

type Schema struct {
	Type                 string           `json:"type"`
	Properties           map[string]Param `json:"properties"`
	Required             []string         `json:"required,omitempty"`
	AdditionalProperties bool             `json:"additionalProperties"`
}

type Tool struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	InputSchema Schema   `json:"inputSchema"`
	command     []string // CLI words before the flags
}

// Argv validates the arguments against the schema and builds the command
// line. Positionals go last, after "--", so a value starting with a dash is
// never read as a flag.
func (t Tool) Argv(in map[string]any) ([]string, error) {
	for _, r := range t.InputSchema.Required {
		if _, ok := in[r]; !ok {
			return nil, fmt.Errorf("argument %q is required", r)
		}
	}
	keys := make([]string, 0, len(in))
	for k := range in {
		if _, ok := t.InputSchema.Properties[k]; !ok {
			return nil, fmt.Errorf("unknown argument %q for %s", k, t.Name)
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	argv := append([]string{}, t.command...)
	var rest []string
	for _, k := range keys {
		p := t.InputSchema.Properties[k]
		vals, err := asStrings(k, p, in[k])
		if err != nil {
			return nil, err
		}
		switch {
		case p.flag == "":
			rest = append(rest, vals...)
		case p.Type == "boolean":
			if vals[0] == "true" {
				argv = append(argv, p.flag)
			}
		default:
			for _, v := range vals {
				argv = append(argv, p.flag, v)
			}
		}
	}
	if len(rest) > 0 {
		argv = append(append(argv, "--"), rest...)
	}
	return argv, nil
}

func asStrings(key string, p Param, v any) ([]string, error) {
	bad := func(want string) error { return fmt.Errorf("argument %q must be %s", key, want) }
	switch p.Type {
	case "string":
		s, ok := v.(string)
		if !ok || s == "" {
			return nil, bad("a non-empty string")
		}
		if len(p.Enum) > 0 && !contains(p.Enum, s) {
			return nil, bad("one of " + strings.Join(p.Enum, ", "))
		}
		return []string{s}, nil
	case "number", "integer":
		f, ok := v.(float64)
		if !ok || (p.Type == "integer" && f != float64(int64(f))) {
			return nil, bad("a " + p.Type)
		}
		return []string{strconv.FormatFloat(f, 'f', -1, 64)}, nil
	case "boolean":
		b, ok := v.(bool)
		if !ok {
			return nil, bad("a boolean")
		}
		return []string{strconv.FormatBool(b)}, nil
	case "array":
		list, ok := v.([]any)
		if !ok {
			return nil, bad("an array of strings")
		}
		out := make([]string, 0, len(list))
		for _, e := range list {
			s, ok := e.(string)
			if !ok {
				return nil, bad("an array of strings")
			}
			out = append(out, s)
		}
		return out, nil
	}
	return nil, bad(p.Type)
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func str(desc, flag string) Param  { return Param{Type: "string", Description: desc, flag: flag} }
func num(desc, flag string) Param  { return Param{Type: "number", Description: desc, flag: flag} }
func integer(d, flag string) Param { return Param{Type: "integer", Description: d, flag: flag} }
func boolean(d, flag string) Param { return Param{Type: "boolean", Description: d, flag: flag} }
func list(d, flag string) Param {
	return Param{Type: "array", Description: d, Items: &Param{Type: "string"}, flag: flag}
}

const storeDesc = "store code, slug or name; prices and stock are for this store (default: the configured store)"

// Tools are the commands exposed to agents. Everything is a read.
var Tools = []Tool{
	{
		Name: "search", command: []string{"search"},
		Description: "Search obramat.es products, or browse a category, with the site's filters. Mirrors `obramat search`.",
		InputSchema: Schema{Type: "object", Properties: map[string]Param{
			"keywords":  str("what to search for (omit when browsing a category)", ""),
			"category":  str("category path from category_list, instead of keywords", "--category"),
			"brand":     list("brands", "--brand"),
			"filter":    list("facet CODE=VALUE pairs from `obramat search filters`", "--filter"),
			"min_price": num("minimum price in EUR", "--min-price"),
			"max_price": num("maximum price in EUR", "--max-price"),
			"sort":      {Type: "string", Description: "result order", Enum: []string{"relevance", "price_asc", "price_desc"}, flag: "--sort"},
			"store":     str(storeDesc, "--store"),
			"in_stock":  boolean("only products in stock at the store", "--in-stock"),
			"page":      integer("result page, 1 to 10", "--page"),
			"limit":     integer("at most this many products", "--limit"),
		}},
	},
	{
		Name: "product_show", command: []string{"product", "show"},
		Description: "One product's price, stock at the store, delivery options, EAN and characteristics. Mirrors `obramat product show`.",
		InputSchema: Schema{Type: "object", Required: []string{"product"}, Properties: map[string]Param{
			"product": str("product reference (Ref. number) or URL", ""),
			"store":   str(storeDesc, "--store"),
		}},
	},
	{
		Name: "store_stock", command: []string{"store", "stock"},
		Description: "A product's stock per store: at one store, at the six stores nearest a point, or at all stores. Mirrors `obramat store stock`.",
		InputSchema: Schema{Type: "object", Required: []string{"product"}, Properties: map[string]Param{
			"product": str("product reference or URL", ""),
			"store":   str("one store: code, slug or name", "--store"),
			"lat":     num("latitude: list the six stores nearest this point (with lng)", "--lat"),
			"lng":     num("longitude", "--lng"),
			"all":     boolean("every store (about 15 requests)", "--all"),
		}},
	},
	{
		Name: "store_list", command: []string{"store", "list"},
		Description: "Every Obramat store with its code, city and coordinates. Mirrors `obramat store list`.",
		InputSchema: Schema{Type: "object", Properties: map[string]Param{}},
	},
	{
		Name: "store_near", command: []string{"store", "near"},
		Description: "The six stores nearest a point, with opening state. Mirrors `obramat store near`.",
		InputSchema: Schema{Type: "object", Required: []string{"lat", "lng"}, Properties: map[string]Param{
			"lat": num("latitude", "--lat"),
			"lng": num("longitude", "--lng"),
		}},
	},
	{
		Name: "category_list", command: []string{"category", "list"},
		Description: "Category paths for search's category argument. Mirrors `obramat category list`.",
		InputSchema: Schema{Type: "object", Properties: map[string]Param{
			"parent": str("only the direct children of this path", "--parent"),
			"depth":  integer("only paths this many levels deep or less", "--depth"),
		}},
	},
	{
		Name: "watch_check", command: []string{"watch", "check"},
		Description: "Run the saved watches and return the events they produced (empty list: nothing changed). Mirrors `obramat watch check`.",
		InputSchema: Schema{Type: "object", Properties: map[string]Param{
			"names": list("watch names; all due watches when omitted", ""),
			"all":   boolean("run every watch, due or not", "--all"),
		}},
	},
}

func lookup(name string) (Tool, bool) {
	for _, t := range Tools {
		if t.Name == name {
			return t, true
		}
	}
	return Tool{}, false
}

func Names() []string {
	out := make([]string, len(Tools))
	for i, t := range Tools {
		out[i] = t.Name
	}
	return out
}
