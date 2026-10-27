package direct

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeRg installs an rg on PATH that prints ripgrep's --json messages for
// the given (path, line, text, kind) records and records its arguments.
func fakeRg(t *testing.T, records [][4]string) (argsFile string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake")
	}
	dir := t.TempDir()
	var out strings.Builder
	for _, r := range records {
		path := map[string]string{"text": r[0]}
		if strings.HasPrefix(r[0], "b64:") {
			path = map[string]string{"bytes": strings.TrimPrefix(r[0], "b64:")}
		}
		msg := map[string]any{"type": r[3], "data": map[string]any{"path": path, "lines": map[string]string{"text": r[2] + "\n"}, "line_number": json.Number(r[1])}}
		if r[3] == "begin" || r[3] == "end" {
			msg["data"] = map[string]any{"path": path}
		}
		b, _ := json.Marshal(msg)
		out.Write(b)
		out.WriteString("\n")
	}
	data := filepath.Join(dir, "out.jsonl")
	if err := os.WriteFile(data, []byte(out.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	argsFile = filepath.Join(dir, "args")
	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$@\" > %q\ncat %q\n", argsFile, data)
	if err := os.WriteFile(filepath.Join(dir, "rg"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return argsFile
}

func grepCall(t *testing.T, r *Runner, args map[string]any) string {
	t.Helper()
	b, _ := json.Marshal(args)
	c := ToolCall{ID: "g"}
	c.Function.Name, c.Function.Arguments = "grep", string(b)
	return r.runTool(context.Background(), c)
}

// With ripgrep installed, grep must not show files the other tools refuse:
// ripgrep searches credentials.json and *.pem (it skips only hidden and
// ignored files), so its results are filtered by the same rule.
func TestGrepWithRipgrepMasksSecretPaths(t *testing.T) {
	r, root, _ := newRunner(t, &scripted{}, ApprovalAuto)
	for _, f := range []string{"app.go", "credentials.json", "certs/server.pem", ".env.example", "a.go"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, f)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, f), []byte("token\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	abs := func(f string) string { return filepath.Join(root, f) }
	argsFile := fakeRg(t, [][4]string{
		{abs("app.go"), "", "", "begin"},
		{abs("app.go"), "3", "func f() {", "context"},
		{abs("app.go"), "4", "\ttoken := load()", "match"},
		{abs("app.go"), "9", "\treturn token", "match"},
		{abs("app.go"), "", "", "end"},
		{abs("credentials.json"), "1", `{"token": "s3cr3t"}`, "match"},
		{abs("certs/server.pem"), "2", "token-ish PEM line", "match"},
		{abs(".env.example"), "1", "TOKEN=", "match"},
		{"b64:" + base64.StdEncoding.EncodeToString([]byte(abs("a.go"))), "1", "token", "match"},
	})

	out := grepCall(t, r, map[string]any{"pattern": "token", "context": 1})
	for _, want := range []string{"app.go-3-func f() {", "app.go:4:\ttoken := load()", "--\napp.go:9:\treturn token", ".env.example:1:TOKEN=", "a.go:1:token"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	for _, secret := range []string{"credentials.json", "s3cr3t", "server.pem"} {
		if strings.Contains(out, secret) {
			t.Errorf("secret path %s shown:\n%s", secret, out)
		}
	}
	args, _ := os.ReadFile(argsFile)
	if !strings.Contains(string(args), "--json") {
		t.Errorf("ripgrep not asked for JSON: %s", args)
	}

	if out := grepCall(t, r, map[string]any{"pattern": "token", "output_mode": "files"}); out != "app.go\n.env.example\na.go" {
		t.Errorf("files mode:\n%s", out)
	}
	if out := grepCall(t, r, map[string]any{"pattern": "token", "output_mode": "count"}); out != "app.go:2\n.env.example:1\na.go:1" {
		t.Errorf("count mode:\n%s", out)
	}
	fakeRg(t, [][4]string{{abs("credentials.json"), "1", "token", "match"}})
	if out := grepCall(t, r, map[string]any{"pattern": "token"}); out != "(no matches)" {
		t.Errorf("only secret matches:\n%s", out)
	}
}
