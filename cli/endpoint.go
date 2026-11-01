package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"

	"github.com/infoe-labs/pikvm-cli/lib/pikvm"
	"github.com/spf13/cobra"
)

func runEndpoint(cmd *cobra.Command, cfg pikvm.Config, ep pikvm.Endpoint) error {
	client, err := newClient(cfg)
	if err != nil {
		return err
	}
	q := url.Values{}
	path := ep.Path
	for _, param := range ep.Parameters {
		name := strings.TrimSuffix(param, "*")
		flag := ep.QueryFlag(name)
		v, _ := cmd.Flags().GetString(flag)
		if name == "id" && strings.Contains(path, "{id}") {
			if v == "" || strings.ContainsAny(v, "/?#") {
				return fmt.Errorf("invalid system id")
			}
			path = strings.ReplaceAll(path, "{id}", url.PathEscape(v))
			continue
		}
		if cmd.Flags().Changed(flag) {
			q.Set(name, v)
		}
	}
	input, _ := cmd.Flags().GetString("input")
	data, _ := cmd.Flags().GetString("body")
	output, _ := cmd.Flags().GetString("output")
	explicit := cmd.Flags().Changed("input") || cmd.Flags().Changed("body")
	body, closeBody, err := openInput(cmd, input, data)
	if err != nil {
		return err
	}
	defer closeBody()
	ct := ep.ContentType
	if len(ep.BodyParameters) > 0 {
		fields := map[string]any{}
		form := url.Values{}
		if ep.Group == "auth" {
			form.Set("user", cfg.Username)
			form.Set("passwd", cfg.Password)
		}
		for _, param := range ep.BodyParameters {
			changed := cmd.Flags().Changed(param.Flag)
			if explicit && changed {
				return fmt.Errorf("--%s cannot be combined with --body or --input", param.Flag)
			}
			if explicit {
				continue
			}
			if param.Required && !changed {
				return fmt.Errorf("--%s is required unless --body or --input is provided", param.Flag)
			}
			if !changed {
				continue
			}
			v, _ := cmd.Flags().GetString(param.Flag)
			if ct == "application/x-www-form-urlencoded" {
				form.Set(param.Name, v)
				continue
			}
			var value any = v
			if param.Type == "bool" {
				value, err = parseAPIBool(v)
				if err != nil {
					return fmt.Errorf("--%s expects a boolean value", param.Flag)
				}
			}
			setJSONField(fields, param.Name, value)
		}
		if !explicit {
			if ct == "application/x-www-form-urlencoded" {
				body = strings.NewReader(form.Encode())
			} else {
				encoded, e := json.Marshal(fields)
				if e != nil {
					return e
				}
				body = strings.NewReader(string(encoded))
			}
		}
	} else if ep.BodyRequired && !explicit {
		return fmt.Errorf("this operation requires --body or --input")
	}
	if ct == "" && explicit {
		ct = "application/json"
	}
	resp, err := client.Request(cmd.Context(), ep.Method, path, q, body, ct)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if ep.Group == "auth" && ep.Name == "login" {
		for _, cookie := range resp.Cookies() {
			if cookie.Name == "auth_token" {
				data, e := json.Marshal(map[string]string{"auth_token": cookie.Value})
				if e != nil {
					return e
				}
				resp.Body = io.NopCloser(strings.NewReader(string(data)))
				resp.Header.Set("Content-Type", "application/json")
				resp.StatusCode = 200
				break
			}
		}
	}
	return writeResponse(cmd, resp, output)
}
func setJSONField(dst map[string]any, path string, value any) {
	parts := strings.Split(path, ".")
	for _, part := range parts[:len(parts)-1] {
		next, ok := dst[part].(map[string]any)
		if !ok {
			next = map[string]any{}
			dst[part] = next
		}
		dst = next
	}
	dst[parts[len(parts)-1]] = value
}
func parseAPIBool(value string) (bool, error) {
	switch strings.ToLower(value) {
	case "yes":
		return true, nil
	case "no":
		return false, nil
	}
	return strconv.ParseBool(value)
}
