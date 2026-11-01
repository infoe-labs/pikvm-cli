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
	for _, parameter := range ep.Parameters {
		name := strings.TrimSuffix(parameter, "*")
		flag := ep.QueryFlag(name)
		if cmd.Flags().Changed(flag) || name == "id" && strings.Contains(ep.Path, "{id}") {
			value, _ := cmd.Flags().GetString(flag)
			q.Set(name, value)
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
	if explicit && body == nil {
		body = strings.NewReader("")
	}
	fields := map[string]any{}
	for _, parameter := range ep.BodyParameters {
		if !cmd.Flags().Changed(parameter.Flag) {
			continue
		}
		if explicit {
			return fmt.Errorf("--%s cannot be combined with --body or --input", parameter.Flag)
		}
		value, _ := cmd.Flags().GetString(parameter.Flag)
		var field any = value
		if parameter.Type == "bool" {
			field, err = parseAPIBool(value)
			if err != nil {
				return fmt.Errorf("--%s expects a boolean value", parameter.Flag)
			}
		}
		fields[parameter.Name] = field
	}
	resp, err := client.Invoke(cmd.Context(), ep, pikvm.OperationInput{Parameters: q, Fields: fields, Body: body})
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
func parseAPIBool(value string) (bool, error) {
	switch strings.ToLower(value) {
	case "yes":
		return true, nil
	case "no":
		return false, nil
	}
	return strconv.ParseBool(value)
}
