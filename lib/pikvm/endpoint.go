package pikvm

import "strings"

// Endpoint describes an audited API operation. Parameters ending in * are required.
// BodyParameters distinguish form/JSON fields from query parameters.
type Endpoint struct {
	Group, Name, Method, Path, Description  string
	Parameters                              []string
	BodyParameters                          []BodyParameter
	ContentType                             string
	BodyRequired, StreamResponse, WebSocket bool
}

// BodyParameter identifies a form field or dotted JSON path and its CLI flag.
type BodyParameter struct {
	Name, Flag, Type string
	Required         bool
}

// QueryFlag returns a CLI flag name without shadowing device connection flags.
func (e Endpoint) QueryFlag(name string) string {
	name = strings.TrimSuffix(name, "*")
	if e.Group == "msd" {
		switch name {
		case "url":
			return "source-url"
		case "user":
			return "remote-user"
		case "insecure":
			return "remote-insecure"
		}
	}
	return strings.ReplaceAll(name, "_", "-")
}
