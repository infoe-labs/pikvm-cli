// Package cli exposes the Cobra command tree for embedding and testing.
package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/infoe-labs/pikvm-cli/lib/pikvm"
	"github.com/spf13/cobra"
)

// NewRootCommand creates an independent command tree. Environment defaults use PIKVM_*.
func NewRootCommand() *cobra.Command {
	cfg := pikvm.Config{URL: os.Getenv("PIKVM_URL"), Username: os.Getenv("PIKVM_USER"), Password: os.Getenv("PIKVM_PASSWORD"), Token: os.Getenv("PIKVM_TOKEN")}
	root := &cobra.Command{Use: "pikvm-cli", Short: "Control PiKVM from terminals and automation", SilenceUsage: true, SilenceErrors: true}
	f := root.PersistentFlags()
	f.StringVar(&cfg.URL, "url", cfg.URL, "PiKVM base URL (PIKVM_URL)")
	f.StringVar(&cfg.Username, "user", cfg.Username, "Username (PIKVM_USER)")
	f.StringVar(&cfg.Password, "password", cfg.Password, "Password, including OTP if needed (PIKVM_PASSWORD)")
	f.StringVar(&cfg.Token, "token", cfg.Token, "Session token (PIKVM_TOKEN)")
	f.BoolVar(&cfg.Insecure, "insecure", false, "Disable TLS certificate verification")
	f.DurationVar(&cfg.Timeout, "request-timeout", 30*time.Second, "HTTP timeout; use 0s for unbounded transfers")
	f.StringVar(&cfg.AuthMode, "auth-mode", "headers", "Authentication mode: headers or basic")
	groups := map[string]*cobra.Command{}
	for _, ep := range pikvm.Endpoints() {
		group := groups[ep.Group]
		if group == nil {
			group = &cobra.Command{Use: ep.Group, Short: "PiKVM " + ep.Group + " operations"}
			root.AddCommand(group)
			groups[ep.Group] = group
		}
		ep := ep
		cmd := &cobra.Command{Use: ep.Name, Short: ep.Description, Args: cobra.NoArgs}
		if ep.WebSocket {
			addSocketFlags(cmd)
			cmd.RunE = func(cmd *cobra.Command, args []string) error { return runSocket(cmd, cfg, ep.Path, nil, false) }
			group.AddCommand(cmd)
			continue
		}
		for _, param := range ep.Parameters {
			name := strings.TrimSuffix(param, "*")
			flag := ep.QueryFlag(name)
			def := ""
			if name == "id" && strings.Contains(ep.Path, "{id}") {
				def = "0"
			}
			cmd.Flags().String(flag, def, "API parameter: "+name)
			if strings.HasSuffix(param, "*") {
				_ = cmd.MarkFlagRequired(flag)
			}
		}
		for _, param := range ep.BodyParameters {
			cmd.Flags().String(param.Flag, "", "Body field: "+param.Name+" ("+param.Type+")")
		}
		cmd.Flags().String("input", "", "Read request body from file, or - for stdin")
		cmd.Flags().String("body", "", "Literal request body")
		cmd.Flags().StringP("output", "o", "", "Write response to file, or - for stdout")
		cmd.MarkFlagsMutuallyExclusive("input", "body")
		cmd.RunE = func(cmd *cobra.Command, args []string) error { return runEndpoint(cmd, cfg, ep) }
		group.AddCommand(cmd)
	}
	addSocketCommands(groups["events"], groups["media"], &cfg)
	var params []string
	var input, output, body, contentType string
	raw := &cobra.Command{Use: "request METHOD /path", Short: "Call any HTTP API endpoint", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		client, e := newClient(cfg)
		if e != nil {
			return e
		}
		q := url.Values{}
		for _, p := range params {
			k, v, ok := strings.Cut(p, "=")
			if !ok || k == "" {
				return fmt.Errorf("query must be key=value")
			}
			q.Add(k, v)
		}
		src, done, e := openInput(cmd, input, body)
		if e != nil {
			return e
		}
		defer done()
		r, e := client.Request(cmd.Context(), strings.ToUpper(args[0]), args[1], q, src, contentType)
		if e != nil {
			return e
		}
		defer r.Body.Close()
		return writeResponse(cmd, r, output)
	}}
	raw.Flags().StringArrayVarP(&params, "query", "q", nil, "Query parameter key=value (repeatable)")
	raw.Flags().StringVar(&input, "input", "", "Body file or - for stdin")
	raw.Flags().StringVar(&body, "body", "", "Literal body")
	raw.MarkFlagsMutuallyExclusive("input", "body")
	raw.Flags().StringVarP(&output, "output", "o", "", "Response file or - for stdout")
	raw.Flags().StringVar(&contentType, "content-type", "application/json", "Request content type")
	root.AddCommand(raw)
	return root
}
func newClient(cfg pikvm.Config) (*pikvm.Client, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("set --url or PIKVM_URL")
	}
	if cfg.Timeout < 0 {
		return nil, fmt.Errorf("request timeout cannot be negative")
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = -1
	}
	return pikvm.New(cfg)
}
func openInput(cmd *cobra.Command, path, data string) (io.Reader, func(), error) {
	if path == "-" {
		return cmd.InOrStdin(), func() {}, nil
	}
	if path != "" {
		f, e := os.Open(path)
		if e != nil {
			return nil, func() {}, e
		}
		return f, func() { _ = f.Close() }, nil
	}
	if data != "" {
		return strings.NewReader(data), func() {}, nil
	}
	return nil, func() {}, nil
}
func writeResponse(cmd *cobra.Command, r *http.Response, path string) error {
	var dst io.Writer = cmd.OutOrStdout()
	if path != "" && path != "-" {
		f, e := os.Create(path)
		if e != nil {
			return e
		}
		_, copyErr := io.Copy(f, r.Body)
		closeErr := f.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	}
	if strings.Contains(r.Header.Get("Content-Type"), "ndjson") {
		return copyNDJSON(dst, r.Body)
	}
	if strings.Contains(r.Header.Get("Content-Type"), "json") && r.StatusCode != 204 {
		var value any
		d := json.NewDecoder(r.Body)
		d.UseNumber()
		if e := d.Decode(&value); e != nil {
			return e
		}
		enc := json.NewEncoder(dst)
		enc.SetIndent("", "  ")
		return enc.Encode(value)
	}
	_, e := io.Copy(dst, r.Body)
	return e
}

// copyNDJSON relays every progress record and surfaces late server failures.
func copyNDJSON(dst io.Writer, src io.Reader) error {
	reader := bufio.NewReader(src)
	var failed bool
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			if _, e := dst.Write(line); e != nil {
				return e
			}
			var record struct {
				OK *bool `json:"ok"`
			}
			if json.Unmarshal(line, &record) == nil && record.OK != nil && !*record.OK {
				failed = true
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}
	if failed {
		return fmt.Errorf("PiKVM streaming operation failed; see response records")
	}
	return nil
}
