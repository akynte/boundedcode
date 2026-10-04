package main

import "testing"

func TestPythonDistsFromLocks(t *testing.T) {
	t.Chdir("../..")
	want := map[string]struct{ in, out []string }{
		"adapter": {in: []string{"openhands-sdk", "openhands-tools", "orjson", "certifi", "func-timeout", "lmnr"},
			out: []string{"pytest", "lmnr-claude-code-proxy", "bc-openhands-adapter", "pyobjc-core"}},
		"serena": {in: []string{"serena-agent", "pillow", "anthropic"},
			out: []string{"pystray", "python-xlib", "bc-serena-env"}},
	}
	for _, env := range pyEnvs() {
		dists, err := pythonDists(env)
		if err != nil {
			t.Fatal(err)
		}
		got := map[string]pyDist{}
		for _, d := range dists {
			got[d.name] = d
		}
		for _, n := range want[env.key].in {
			d, ok := got[n]
			if !ok {
				t.Errorf("%s: %s missing", env.key, n)
				continue
			}
			if d.url == "" || len(d.sha256) != 64 {
				t.Errorf("%s: %s has no locked artifact: %+v", env.key, n, d)
			}
		}
		for _, n := range want[env.key].out {
			if _, ok := got[n]; ok {
				t.Errorf("%s: %s must not be listed", env.key, n)
			}
		}
	}
}
