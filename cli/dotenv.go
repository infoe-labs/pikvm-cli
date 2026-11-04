package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
	"time"

	"github.com/infoe-labs/pikvm-cli/lib/pikvm"
	"github.com/spf13/cobra"
)

const (
	// envFileFlag selects the dotenv file; setting it to "" disables file loading.
	envFileFlag = "env-file"
	// defaultEnvFile is read from the working directory when no override is given.
	defaultEnvFile = ".env"
	// envFileVar overrides the dotenv path without a flag. Setting it empty disables loading.
	envFileVar = "PIKVM_ENV_FILE"
	// maxEnvFileBytes bounds how much of a dotenv file is read.
	maxEnvFileBytes = 1 << 20
	// maxEnvKeyLen bounds a accepted variable name length.
	maxEnvKeyLen = 128
	// insecureEnvVar, authModeEnvVar and timeoutEnvVar extend the PIKVM_* namespace
	// to the remaining global settings so a dotenv file can configure a device fully.
	insecureEnvVar = "PIKVM_INSECURE"
	authModeEnvVar = "PIKVM_AUTH_MODE"
	timeoutEnvVar  = "PIKVM_REQUEST_TIMEOUT"
)

// dotenv holds assignments parsed from a dotenv file. Only PIKVM_* keys are consumed.
type dotenv map[string]string

// loadDotEnv reads assignments from path. A missing file yields no values and no error;
// an unreadable or malformed file fails. Errors never repeat the path or any value.
func loadDotEnv(path string) (dotenv, error) {
	if strings.TrimSpace(path) == "" {
		return dotenv{}, nil
	}
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return dotenv{}, nil
		}
		return nil, fmt.Errorf("cannot read the environment file; check its path and permissions")
	}
	defer file.Close()
	return parseDotEnv(file)
}

// parseDotEnv reads KEY=VALUE assignments, tolerating an "export " prefix, comments,
// blank lines, single and double quoting, and CRLF endings.
func parseDotEnv(reader io.Reader) (dotenv, error) {
	values := dotenv{}
	scanner := bufio.NewScanner(io.LimitReader(reader, maxEnvFileBytes))
	for number := 1; scanner.Scan(); number++ {
		key, value, ok, err := parseDotEnvLine(scanner.Text(), number)
		if err != nil {
			return nil, err
		}
		if ok {
			values[key] = value
		}
	}
	if scanner.Err() != nil {
		return nil, fmt.Errorf("cannot read the environment file; each line must stay within %d bytes", maxEnvFileBytes)
	}
	return values, nil
}

// parseDotEnvLine reports the assignment carried by one line, if any.
func parseDotEnvLine(text string, number int) (key, value string, ok bool, err error) {
	line := strings.TrimSpace(strings.TrimPrefix(text, "\ufeff"))
	if rest, ok := strings.CutPrefix(line, "export"); ok {
		if trimmed := strings.TrimLeft(rest, " \t"); trimmed != rest {
			line = trimmed
		}
	}
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false, nil
	}
	key, rest, found := strings.Cut(line, "=")
	if !found {
		return "", "", false, fmt.Errorf("line %d of the environment file is not NAME=VALUE", number)
	}
	key = strings.TrimSpace(key)
	if !validEnvKey(key) {
		return "", "", false, fmt.Errorf("line %d of the environment file uses the unsupported name %q", number, key)
	}
	value, err = parseDotEnvValue(strings.TrimSpace(rest), number, key)
	if err != nil {
		return "", "", false, err
	}
	return key, value, true, nil
}

// validEnvKey accepts portable shell variable names.
func validEnvKey(key string) bool {
	if key == "" || len(key) > maxEnvKeyLen {
		return false
	}
	for i, r := range key {
		if r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || i > 0 && r >= '0' && r <= '9' {
			continue
		}
		return false
	}
	return true
}

// parseDotEnvValue unwraps one value. Errors name the line and variable, never the value.
func parseDotEnvValue(text string, number int, key string) (string, error) {
	if text == "" {
		return "", nil
	}
	switch text[0] {
	case '\'':
		end := strings.Index(text[1:], "'")
		if end < 0 {
			return "", unterminatedQuote(number, key)
		}
		return text[1 : end+1], nil
	case '"':
		var value strings.Builder
		for i := 1; i < len(text); i++ {
			switch text[i] {
			case '\\':
				if i+1 == len(text) {
					return "", unterminatedQuote(number, key)
				}
				i++
				if escaped, ok := unescapeDotEnv(text[i]); ok {
					value.WriteString(escaped)
				} else {
					value.WriteByte(text[i])
				}
			case '"':
				if rest := strings.TrimSpace(text[i+1:]); rest != "" && !strings.HasPrefix(rest, "#") {
					return "", fmt.Errorf("line %d of the environment file has trailing text after the quoted %s value", number, key)
				}
				return value.String(), nil
			default:
				value.WriteByte(text[i])
			}
		}
		return "", unterminatedQuote(number, key)
	}
	// Unquoted values keep "#"; only whitespace-preceded # starts a comment.
	value, _, _ := strings.Cut(text, " #")
	return strings.TrimSpace(value), nil
}

func unterminatedQuote(number int, key string) error {
	return fmt.Errorf("line %d of the environment file has an unterminated quote for %s", number, key)
}

func unescapeDotEnv(char byte) (string, bool) {
	switch char {
	case 'n':
		return "\n", true
	case 'r':
		return "\r", true
	case 't':
		return "\t", true
	case '\\', '"', '\'', '$':
		return string(char), true
	}
	return "", false
}

// settingSources resolves a setting from the process environment, then the dotenv file.
// Explicit process values always win so an inherited environment overrides a stray file.
type settingSources struct{ file dotenv }

func (s settingSources) lookup(key string) (string, bool) {
	if value, ok := os.LookupEnv(key); ok && strings.TrimSpace(value) != "" {
		return value, true
	}
	if value, ok := s.file[key]; ok && strings.TrimSpace(value) != "" {
		return value, true
	}
	return "", false
}

// resolveEnvFile picks the dotenv path: the flag when given, else PIKVM_ENV_FILE, else .env.
func resolveEnvFile(cmd *cobra.Command, flagValue string) string {
	if cmd.Flags().Changed(envFileFlag) {
		return flagValue
	}
	if value, ok := os.LookupEnv(envFileVar); ok && strings.TrimSpace(value) != "" {
		return value
	}
	return flagValue
}

// warnUnsafePermissions warns once when the dotenv file is readable beyond its owner.
// The path is deliberately omitted from the message.
func warnUnsafePermissions(cmd *cobra.Command, path string) {
	if strings.TrimSpace(path) == "" {
		return
	}
	if info, err := os.Stat(path); err == nil && info.Mode().Perm()&0o077 != 0 {
		fmt.Fprintln(cmd.ErrOrStderr(), "warning: the environment file is readable by group or others; restrict it to the owner")
	}
}

// parseEnvBool accepts the boolean spellings people actually write in dotenv files.
func parseEnvBool(value string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "y", "yes", "on":
		return true, nil
	case "n", "no", "off":
		return false, nil
	}
	return parseAPIBool(value)
}

// applyEnvSettings fills unset global settings from the environment and dotenv file.
// Explicit flags always win, so precedence is flag, then process environment, then file.
func applyEnvSettings(cmd *cobra.Command, cfg *pikvm.Config, envFile string) error {
	path := resolveEnvFile(cmd, envFile)
	file, err := loadDotEnv(path)
	if err != nil {
		return err
	}
	warnUnsafePermissions(cmd, path)
	sources := settingSources{file: file}
	for _, setting := range []struct {
		flag, env string
		target    *string
	}{
		{"url", "PIKVM_URL", &cfg.URL},
		{"user", "PIKVM_USER", &cfg.Username},
		{"password", "PIKVM_PASSWORD", &cfg.Password},
		{"token", "PIKVM_TOKEN", &cfg.Token},
	} {
		if cmd.Flags().Changed(setting.flag) {
			continue
		}
		if value, ok := sources.lookup(setting.env); ok {
			*setting.target = value
		}
	}
	if !cmd.Flags().Changed("auth-mode") {
		if value, ok := sources.lookup(authModeEnvVar); ok {
			if value != "headers" && value != "basic" {
				return fmt.Errorf("%s must be headers or basic", authModeEnvVar)
			}
			cfg.AuthMode = value
		}
	}
	if !cmd.Flags().Changed("insecure") {
		if value, ok := sources.lookup(insecureEnvVar); ok {
			insecure, err := parseEnvBool(value)
			if err != nil {
				return fmt.Errorf("%s must be a boolean such as true or false", insecureEnvVar)
			}
			cfg.Insecure = insecure
		}
	}
	if !cmd.Flags().Changed("request-timeout") {
		if value, ok := sources.lookup(timeoutEnvVar); ok {
			timeout, err := time.ParseDuration(value)
			if err != nil {
				return fmt.Errorf("%s must be a Go duration such as 30s or 0s for no timeout", timeoutEnvVar)
			}
			cfg.Timeout = timeout
		}
	}
	return nil
}
