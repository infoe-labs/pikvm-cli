package cli

import (
	"bytes"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/infoe-labs/pikvm-cli/lib/pikvm"
	"github.com/spf13/cobra"
)

// writeEnvFile stores a synthetic dotenv fixture and returns its path.
func writeEnvFile(t *testing.T, body string, perm os.FileMode) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "synthetic.env")
	if err := os.WriteFile(path, []byte(body), perm); err != nil {
		t.Fatal(err)
	}
	return path
}

func executeRoot(out, errOut io.Writer, args ...string) error {
	root := NewRootCommand()
	root.SetOut(out)
	root.SetErr(errOut)
	root.SetIn(strings.NewReader(""))
	root.SetArgs(args)
	return root.Execute()
}

func TestParseDotEnvValues(t *testing.T) {
	for _, tc := range []struct{ name, input, key, want string }{
		{"plain", "PIKVM_URL=https://pikvm.invalid", "PIKVM_URL", "https://pikvm.invalid"},
		{"export prefix", "export PIKVM_URL=https://pikvm.invalid", "PIKVM_URL", "https://pikvm.invalid"},
		{"export with tab", "export\tPIKVM_USER=red", "PIKVM_USER", "red"},
		{"spacing", "  PIKVM_USER  =  red  ", "PIKVM_USER", "red"},
		{"single quotes", "PIKVM_PASSWORD='syn#thetic '", "PIKVM_PASSWORD", "syn#thetic "},
		{"single quotes keep backslash", `PIKVM_PASSWORD='a\tb'`, "PIKVM_PASSWORD", `a\tb`},
		{"double quotes", `PIKVM_PASSWORD="syn thetic"`, "PIKVM_PASSWORD", "syn thetic"},
		{"double quote escape", `PIKVM_PASSWORD="quote\"inside"`, "PIKVM_PASSWORD", `quote"inside`},
		{"double quote tab escape", `PIKVM_PASSWORD="a\tb"`, "PIKVM_PASSWORD", "a\tb"},
		{"double quote literal escape", `PIKVM_PASSWORD="\q"`, "PIKVM_PASSWORD", "q"},
		{"double quote trailing comment", `PIKVM_USER="red" # note`, "PIKVM_USER", "red"},
		{"empty value", "PIKVM_TOKEN=", "PIKVM_TOKEN", ""},
		{"value contains equals", "PIKVM_PASSWORD=a=b", "PIKVM_PASSWORD", "a=b"},
		{"unquoted hash is kept", "PIKVM_TOKEN=raw#value", "PIKVM_TOKEN", "raw#value"},
		{"unquoted inline comment", "PIKVM_TOKEN=raw # note", "PIKVM_TOKEN", "raw"},
		{"scheme keeps slashes", "PIKVM_URL=http://pikvm.invalid:443/", "PIKVM_URL", "http://pikvm.invalid:443/"},
		{"bom prefix", "\ufeffPIKVM_USER=red", "PIKVM_USER", "red"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			values, err := parseDotEnv(strings.NewReader(tc.input + "\n"))
			if err != nil {
				t.Fatal(err)
			}
			if got := values[tc.key]; got != tc.want {
				t.Errorf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestParseDotEnvSkipsAndIgnores(t *testing.T) {
	values, err := parseDotEnv(strings.NewReader("\n   \n# comment\n  # indented comment\nUNRELATED=value\nPIKVM_USER=red\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 2 || values["PIKVM_USER"] != "red" || values["UNRELATED"] != "value" {
		t.Errorf("unexpected values %v", values)
	}
}

func TestParseDotEnvErrors(t *testing.T) {
	for _, tc := range []struct{ name, input, want string }{
		{"unterminated double quote", `PIKVM_PASSWORD="supersecretvalue`, "unterminated quote"},
		{"unterminated single quote", "PIKVM_PASSWORD='supersecretvalue", "unterminated quote"},
		{"dangling escape", `PIKVM_PASSWORD="supersecretvalue\`, "unterminated quote"},
		{"trailing text after quote", `PIKVM_PASSWORD="supersecretvalue";`, "trailing text"},
		{"missing equals", "PIKVM_PASSWORD supersecretvalue", "NAME=VALUE"},
		{"invalid name", "1PIKVM=x", "unsupported name"},
		{"dashed name", "PIKVM-PASSWORD=x", "unsupported name"},
		{"empty name", "=value", "unsupported name"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseDotEnv(strings.NewReader(tc.input))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("unexpected error %v", err)
			}
			if strings.Contains(err.Error(), "supersecretvalue") {
				t.Errorf("error leaked the value: %v", err)
			}
		})
	}
}

func TestParseDotEnvRejectsOversizedLine(t *testing.T) {
	if _, err := parseDotEnv(strings.NewReader("PIKVM_USER=" + strings.Repeat("a", 70000))); err == nil {
		t.Fatal("accepted an oversized line")
	}
}

func TestParseDotEnvEscapes(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{`PIKVM_PASSWORD="a\nb"`, "a\nb"},
		{`PIKVM_PASSWORD="a\rb"`, "a\rb"},
		{`PIKVM_PASSWORD="a\tb"`, "a\tb"},
		{`PIKVM_PASSWORD="a\\b"`, `a\b`},
		{`PIKVM_PASSWORD="a\"b"`, `a"b`},
		{`PIKVM_PASSWORD="a\'b"`, `a'b`},
		{`PIKVM_PASSWORD="a\$b"`, `a$b`},
		{`PIKVM_PASSWORD="a\qb"`, `aqb`},
	} {
		values, err := parseDotEnv(strings.NewReader(tc.input))
		if err != nil {
			t.Fatalf("%s: %v", tc.input, err)
		}
		if values["PIKVM_PASSWORD"] != tc.want {
			t.Errorf("%s = %q want %q", tc.input, values["PIKVM_PASSWORD"], tc.want)
		}
	}
}

func TestLoadDotEnvUnreadable(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "regular")
	if err := os.WriteFile(blocker, []byte("PIKVM_USER=red\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadDotEnv(filepath.Join(blocker, "child.env")); err == nil {
		t.Fatal("read through a regular file")
	}
}

func TestLoadDotEnv(t *testing.T) {
	if values, err := loadDotEnv(""); err != nil || len(values) != 0 {
		t.Errorf("empty path should load nothing, got %v %v", values, err)
	}
	if values, err := loadDotEnv(filepath.Join(t.TempDir(), "absent.env")); err != nil || len(values) != 0 {
		t.Errorf("missing file should load nothing, got %v %v", values, err)
	}
	if _, err := loadDotEnv(t.TempDir()); err == nil {
		t.Error("accepted a directory as a dotenv file")
	}
	values, err := loadDotEnv(writeEnvFile(t, "PIKVM_USER=red\n", 0o600))
	if err != nil || values["PIKVM_USER"] != "red" {
		t.Errorf("unexpected read result %v %v", values, err)
	}
}

func TestParseEnvBool(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  bool
	}{
		{"true", true}, {"TRUE", true}, {"1", true}, {"yes", true}, {"YES", true}, {"on", true}, {" y ", true},
		{"false", false}, {"0", false}, {"no", false}, {"off", false}, {"N", false},
	} {
		got, err := parseEnvBool(tc.input)
		if err != nil || got != tc.want {
			t.Errorf("parseEnvBool(%q) = %v, %v", tc.input, got, err)
		}
	}
	if _, err := parseEnvBool("maybe"); err == nil {
		t.Error("accepted a malformed boolean")
	}
}

func TestValidEnvKey(t *testing.T) {
	for _, key := range []string{"PIKVM_URL", "_X", "A1", strings.Repeat("a", maxEnvKeyLen)} {
		if !validEnvKey(key) {
			t.Errorf("rejected valid name %q", key)
		}
	}
	for _, key := range []string{"", "1A", "A-B", strings.Repeat("a", maxEnvKeyLen+1), "A B"} {
		if validEnvKey(key) {
			t.Errorf("accepted invalid name %q", key)
		}
	}
}

func TestSettingSourcesPrecedence(t *testing.T) {
	t.Setenv("PIKVM_USER", "fromenv")
	sources := settingSources{file: dotenv{"PIKVM_USER": "fromfile", "PIKVM_TOKEN": "  ", "PIKVM_URL": "https://pikvm.invalid"}}
	if got, ok := sources.lookup("PIKVM_USER"); !ok || got != "fromenv" {
		t.Errorf("process environment did not win: %q %v", got, ok)
	}
	if got, ok := sources.lookup("PIKVM_URL"); !ok || got != "https://pikvm.invalid" {
		t.Errorf("dotenv value not used: %q %v", got, ok)
	}
	for _, key := range []string{"PIKVM_TOKEN", "PIKVM_PASSWORD"} {
		if _, ok := sources.lookup(key); ok {
			t.Errorf("blank or absent %s reported as set", key)
		}
	}
}

func TestApplyEnvSettingsReportsBadValues(t *testing.T) {
	for _, body := range []string{
		"PIKVM_AUTH_MODE=teleport",
		"PIKVM_INSECURE=maybe",
		"PIKVM_REQUEST_TIMEOUT=soon",
		"PIKVM_PASSWORD=\"unterminated",
	} {
		cmd := &cobra.Command{}
		var stderr bytes.Buffer
		cmd.SetErr(&stderr)
		cmd.Flags().String(envFileFlag, "", "")
		if err := applyEnvSettings(cmd, &pikvm.Config{}, writeEnvFile(t, body, 0o600)); err == nil {
			t.Errorf("accepted %q", body)
		}
	}
}

func TestApplyEnvSettingsAppliesFileValues(t *testing.T) {
	path := writeEnvFile(t, "PIKVM_URL=https://pikvm.invalid\nPIKVM_USER=red\nPIKVM_PASSWORD=synthetic\nPIKVM_TOKEN=tok\nPIKVM_AUTH_MODE=basic\nPIKVM_INSECURE=yes\nPIKVM_REQUEST_TIMEOUT=5m\n", 0o600)
	cmd := &cobra.Command{}
	cmd.SetErr(io.Discard)
	cmd.Flags().String(envFileFlag, "", "")
	cfg := pikvm.Config{}
	if err := applyEnvSettings(cmd, &cfg, path); err != nil {
		t.Fatal(err)
	}
	if cfg.URL != "https://pikvm.invalid" || cfg.Username != "red" || cfg.Password != "synthetic" || cfg.Token != "tok" || cfg.AuthMode != "basic" || !cfg.Insecure || cfg.Timeout != 5*time.Minute {
		t.Errorf("dotenv settings were not applied: %+v", cfg)
	}
}

func TestApplyEnvSettingsFlagWins(t *testing.T) {
	path := writeEnvFile(t, "PIKVM_URL=https://from-file.invalid\nPIKVM_AUTH_MODE=basic\n", 0o600)
	cmd := &cobra.Command{}
	cmd.SetErr(io.Discard)
	cmd.Flags().String(envFileFlag, "", "")
	cmd.Flags().String("url", "", "")
	cmd.Flags().String("auth-mode", "headers", "")
	_ = cmd.Flags().Set("url", "https://from-flag.invalid")
	_ = cmd.Flags().Set("auth-mode", "headers")
	cfg := pikvm.Config{URL: "https://from-flag.invalid", AuthMode: "headers"}
	if err := applyEnvSettings(cmd, &cfg, path); err != nil {
		t.Fatal(err)
	}
	if cfg.URL != "https://from-flag.invalid" || cfg.AuthMode != "headers" {
		t.Errorf("explicit flags lost to the dotenv file: %+v", cfg)
	}
}

func TestResolveEnvFile(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().String(envFileFlag, defaultEnvFile, "")
	if got := resolveEnvFile(cmd, defaultEnvFile); got != defaultEnvFile {
		t.Errorf("default path = %q", got)
	}
	t.Setenv(envFileVar, "/synthetic/other.env")
	if got := resolveEnvFile(cmd, defaultEnvFile); got != "/synthetic/other.env" {
		t.Errorf("PIKVM_ENV_FILE ignored, got %q", got)
	}
	t.Setenv(envFileVar, "   ")
	if got := resolveEnvFile(cmd, defaultEnvFile); got != defaultEnvFile {
		t.Errorf("blank PIKVM_ENV_FILE should keep the default, got %q", got)
	}
	_ = cmd.Flags().Set(envFileFlag, "")
	if got := resolveEnvFile(cmd, ""); got != "" {
		t.Errorf("explicit empty path did not disable loading, got %q", got)
	}
}

func TestWarnUnsafePermissions(t *testing.T) {
	var stderr bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetErr(&stderr)
	warnUnsafePermissions(cmd, "")
	warnUnsafePermissions(cmd, filepath.Join(t.TempDir(), "absent.env"))
	warnUnsafePermissions(cmd, writeEnvFile(t, "PIKVM_USER=red", 0o600))
	if stderr.Len() != 0 {
		t.Errorf("unexpected warnings: %q", stderr.String())
	}
	warnUnsafePermissions(cmd, writeEnvFile(t, "PIKVM_USER=red", 0o644))
	if !strings.Contains(stderr.String(), "readable by group or others") {
		t.Error("missing permission warning")
	}
	if strings.Contains(stderr.String(), "synthetic.env") {
		t.Error("warning leaked the local filename")
	}
}

// TestCommandUsesDotenv covers URL, credentials and auth mode end to end.
func TestCommandUsesDotenv(t *testing.T) {
	var authorization string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization = r.Header.Get("Authorization")
		if r.URL.Path != "/api/info" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true,"result":{}}`)
	}))
	defer server.Close()
	path := writeEnvFile(t, "PIKVM_URL="+server.URL+"\nPIKVM_USER=red\nPIKVM_PASSWORD=synthetic\nPIKVM_AUTH_MODE=basic\n", 0o600)
	if err := executeRoot(io.Discard, io.Discard, "--env-file", path, "system", "info"); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(authorization, "Basic ") {
		t.Errorf("dotenv auth mode was ignored, Authorization=%q", authorization)
	}
}

func TestCommandInsecureFromEnvironment(t *testing.T) {
	requests := 0
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true,"result":{}}`)
	}))
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.StartTLS()
	defer server.Close()

	if err := executeRoot(io.Discard, io.Discard, "--env-file", "", "--url", server.URL, "system", "info"); err == nil {
		t.Fatal("verified TLS against an untrusted certificate")
	}

	path := writeEnvFile(t, "PIKVM_INSECURE=on\n", 0o600)
	if err := executeRoot(io.Discard, io.Discard, "--env-file", path, "--url", server.URL, "system", "info"); err != nil {
		t.Fatalf("PIKVM_INSECURE from dotenv failed: %v", err)
	}

	t.Setenv(insecureEnvVar, "true")
	if err := executeRoot(io.Discard, io.Discard, "--env-file", "", "--url", server.URL, "system", "info"); err != nil {
		t.Fatalf("PIKVM_INSECURE from the process environment failed: %v", err)
	}

	// An explicit --insecure=false must override the inherited environment value.
	if err := executeRoot(io.Discard, io.Discard, "--env-file", "", "--insecure=false", "--url", server.URL, "system", "info"); err == nil {
		t.Fatal("explicit --insecure=false was overridden by PIKVM_INSECURE")
	}
	if requests < 2 {
		t.Errorf("expected successful insecure requests, got %d", requests)
	}
}

func TestCommandRejectsBadEnvironmentValues(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer server.Close()
	for _, body := range []string{"PIKVM_INSECURE=perhaps", "PIKVM_REQUEST_TIMEOUT=30", "PIKVM_AUTH_MODE=bearer", "PIKVM_USER=\"broken"} {
		path := writeEnvFile(t, body+"\n", 0o600)
		if err := executeRoot(io.Discard, io.Discard, "--env-file", path, "--url", server.URL, "system", "info"); err == nil {
			t.Errorf("accepted %q", body)
		}
	}
}

func TestCommandEnvFileResolution(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true,"result":{}}`)
	}))
	defer server.Close()

	// A .env in the working directory is loaded without any flag.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, defaultEnvFile), []byte("PIKVM_URL="+server.URL+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	if err := executeRoot(io.Discard, io.Discard, "system", "info"); err != nil {
		t.Fatalf("working directory .env was not loaded: %v", err)
	}

	// PIKVM_ENV_FILE selects a different file.
	other := filepath.Join(dir, "other.env")
	if err := os.WriteFile(other, []byte("PIKVM_URL="+server.URL+"\nPIKVM_USER=red\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(envFileVar, other)
	if err := executeRoot(io.Discard, io.Discard, "system", "info"); err != nil {
		t.Fatalf("PIKVM_ENV_FILE was not honoured: %v", err)
	}

	// An explicit empty --env-file disables file loading entirely.
	if err := executeRoot(io.Discard, io.Discard, "--env-file", "", "system", "info"); err == nil {
		t.Error("disabled file loading still found a URL")
	} else if !strings.Contains(err.Error(), "PIKVM_URL") {
		t.Errorf("unexpected missing URL error: %v", err)
	}
}

func TestCommandWarnsOnReadableEnvFile(t *testing.T) {
	var stderr bytes.Buffer
	path := writeEnvFile(t, "PIKVM_URL=https://pikvm.invalid\n", 0o644)
	if err := executeRoot(io.Discard, &stderr, "--env-file", path, "system", "info"); err == nil {
		t.Fatal("expected a connection failure")
	}
	if !strings.Contains(stderr.String(), "readable by group or others") {
		t.Error("permission warning was not shown")
	}
}
