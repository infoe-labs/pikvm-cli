// Package apitest reads the independently audited upstream API inventory for tests.
package apitest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/infoe-labs/pikvm-cli/lib/pikvm"
)

type Operation struct {
	Method, Path, Location string
	Parameters             []string
}

func Inventory(t *testing.T) []Operation {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	var result []Operation
	for _, name := range []string{"upstream.json", "ustreamer.json"} {
		data, e := os.ReadFile(filepath.Join(filepath.Dir(file), "../../api", name))
		if e != nil {
			t.Fatal(e)
		}
		var snapshot struct{ Operations []Operation }
		if e = json.Unmarshal(data, &snapshot); e != nil {
			t.Fatal(e)
		}
		result = append(result, snapshot.Operations...)
	}
	return result
}
func Key(method, path string) string { return method + " " + path }
func Catalog(t *testing.T) map[string]pikvm.Endpoint {
	t.Helper()
	r := map[string]pikvm.Endpoint{}
	for _, e := range pikvm.Endpoints() {
		k := Key(e.Method, e.Path)
		if _, ok := r[k]; ok {
			t.Fatalf("duplicate endpoint %s", k)
		}
		r[k] = e
	}
	return r
}
func AssertSame(t *testing.T, got, want []string) {
	t.Helper()
	g := append([]string(nil), got...)
	w := append([]string(nil), want...)
	sort.Strings(g)
	sort.Strings(w)
	if strings.Join(g, "\x00") != strings.Join(w, "\x00") {
		t.Fatalf("parameters differ: got %v want %v", g, w)
	}
}
func Sample(name string) string {
	if name == "stream" {
		return "false"
	}
	if name == "video" {
		return "jpeg"
	}
	return "value & " + name
}
