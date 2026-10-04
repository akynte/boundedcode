package main

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

// Python environments are not part of the binary. They are installed from a
// uv.lock (exact versions and hashes) and classified by
// scripts/pylicensecheck.py into a generated THIRD_PARTY.md. The SBOM lists
// the distributions that are both reachable from the project's runtime (not
// dev) dependencies in the lock and installed in the reference environment
// (Linux x86-64) according to THIRD_PARTY.md. Platform-only packages for other
// systems, dev tools and excluded packages (uv override markers) drop out.

// pyEnv names one locked Python environment.
type pyEnv struct {
	key, lock, inventory, rel string
}

func pyEnvs() []pyEnv {
	return []pyEnv{
		{"adapter", "adapters/openhands/python/uv.lock", "adapters/openhands/python/THIRD_PARTY.md", "RUNTIME_DEPENDENCY_OF"},
		{"serena", "configs/serena/uv.lock", "configs/serena/THIRD_PARTY.md", "OPTIONAL_DEPENDENCY_OF"},
	}
}

type pyDep struct {
	name   string
	extras []string
}

type lockPkg struct {
	name, version string
	root          bool // the project itself (editable or virtual source)
	url, sha256   string
	wheels        [][2]string // url, sha256
	deps          []pyDep
	optional      map[string][]pyDep
}

type pyDist struct {
	name, version, license, verdict string
	url, sha256                     string
}

var (
	lockKV     = regexp.MustCompile(`^(name|version) = "([^"]*)"`)
	lockDep    = regexp.MustCompile(`\{ name = "([^"]+)"(?:, version = "[^"]*")?(?:, extra = \[([^\]]*)\])?`)
	lockArtURL = regexp.MustCompile(`url = "([^"]+)", hash = "sha256:([0-9a-f]{64})"`)
	lockList   = regexp.MustCompile(`^([A-Za-z0-9_.-]+) = \[`)
	quoted     = regexp.MustCompile(`"([^"]*)"`)
	nonAlnum   = regexp.MustCompile(`[-_.]+`)
)

func normName(n string) string { return nonAlnum.ReplaceAllString(strings.ToLower(n), "-") }

// parseLock reads the fields of a uv.lock the SBOM needs. uv.lock is TOML in
// a fixed layout written by uv, so a line scanner is enough and avoids a
// TOML dependency.
func parseLock(path string) ([]*lockPkg, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var pkgs []*lockPkg
	var cur *lockPkg
	section := "" // "package", "optional", "skip"
	list := ""    // open multi-line array: "dependencies", "wheels", "opt:<extra>", "other"
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		trim := strings.TrimSpace(line)
		switch {
		case trim == "[[package]]":
			cur = &lockPkg{optional: map[string][]pyDep{}}
			pkgs = append(pkgs, cur)
			section, list = "package", ""
			continue
		case strings.HasPrefix(trim, "[package.optional-dependencies]"):
			section, list = "optional", ""
			continue
		case strings.HasPrefix(trim, "["+"package.") || (strings.HasPrefix(trim, "[") && !strings.HasPrefix(line, " ") && strings.HasSuffix(trim, "]") && !strings.Contains(trim, "=")):
			section, list = "skip", ""
			continue
		}
		if cur == nil || section == "skip" {
			continue
		}
		if list != "" {
			if trim == "]" {
				list = ""
				continue
			}
			addListItem(cur, list, trim)
			continue
		}
		if section == "package" {
			if m := lockKV.FindStringSubmatch(line); m != nil {
				if m[1] == "name" {
					cur.name = m[2]
				} else {
					cur.version = m[2]
				}
				continue
			}
			if strings.HasPrefix(line, "source = ") && (strings.Contains(line, "editable =") || strings.Contains(line, "virtual =")) {
				cur.root = true
				continue
			}
			if strings.HasPrefix(line, "sdist = ") {
				if m := lockArtURL.FindStringSubmatch(line); m != nil {
					cur.url, cur.sha256 = m[1], m[2]
				}
				continue
			}
		}
		if m := lockList.FindStringSubmatch(line); m != nil && !strings.HasPrefix(line, " ") {
			name := m[1]
			switch {
			case section == "optional":
				name = "opt:" + name
			case name != "dependencies" && name != "wheels":
				name = "other"
			}
			rest := strings.TrimSpace(line[len(m[0]):])
			if strings.HasSuffix(rest, "]") { // single-line array
				for _, item := range splitInline(strings.TrimSuffix(rest, "]")) {
					addListItem(cur, name, item)
				}
				continue
			}
			list = name
		}
	}
	return pkgs, sc.Err()
}

// splitInline splits the items of a one-line array of inline tables.
func splitInline(s string) []string {
	var out []string
	for part := range strings.SplitSeq(s, "}") {
		if p := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(part), ",")); p != "" {
			out = append(out, p+"}")
		}
	}
	return out
}

func addListItem(p *lockPkg, list, item string) {
	switch {
	case list == "dependencies" || strings.HasPrefix(list, "opt:"):
		m := lockDep.FindStringSubmatch(item)
		if m == nil {
			return
		}
		d := pyDep{name: m[1]}
		for _, q := range quoted.FindAllStringSubmatch(m[2], -1) {
			d.extras = append(d.extras, q[1])
		}
		if list == "dependencies" {
			p.deps = append(p.deps, d)
		} else {
			e := strings.TrimPrefix(list, "opt:")
			p.optional[e] = append(p.optional[e], d)
		}
	case list == "wheels":
		if m := lockArtURL.FindStringSubmatch(item); m != nil {
			p.wheels = append(p.wheels, [2]string{m[1], m[2]})
		}
	}
}

// runtimeClosure returns the normalized names reachable from the root
// package's runtime dependencies (markers ignored, extras followed).
func runtimeClosure(pkgs []*lockPkg) map[string]bool {
	byName := map[string][]*lockPkg{}
	var roots []*lockPkg
	for _, p := range pkgs {
		byName[normName(p.name)] = append(byName[normName(p.name)], p)
		if p.root {
			roots = append(roots, p)
		}
	}
	seen := map[string]bool{}
	var visit func(d pyDep)
	visit = func(d pyDep) {
		n := normName(d.name)
		key := n + "[" + strings.Join(d.extras, ",") + "]"
		if seen[key] {
			return
		}
		seen[key], seen[n] = true, true
		for _, p := range byName[n] {
			for _, x := range p.deps {
				visit(x)
			}
			for _, e := range d.extras {
				for _, x := range p.optional[e] {
					visit(x)
				}
			}
		}
	}
	for _, r := range roots {
		for _, d := range r.deps {
			visit(d)
		}
	}
	out := map[string]bool{}
	for k := range seen {
		if !strings.Contains(k, "[") {
			out[k] = true
		}
	}
	return out
}

// parseInventory reads the generated table of scripts/pylicensecheck.py:
// | verdict | package | version | license |.
func parseInventory(path string) (map[string]pyDist, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	out := map[string]pyDist{}
	for line := range strings.SplitSeq(string(b), "\n") {
		cells := strings.Split(strings.Trim(strings.TrimSpace(line), "|"), "|")
		if len(cells) != 4 {
			continue
		}
		for i := range cells {
			cells[i] = strings.TrimSpace(cells[i])
		}
		if cells[0] == "verdict" || strings.HasPrefix(cells[0], "---") {
			continue
		}
		out[normName(cells[1])+"@"+cells[2]] = pyDist{name: cells[1], version: cells[2], verdict: cells[0], license: cells[3]}
	}
	return out, nil
}

// pythonDists lists the locked runtime distributions of env that are
// installed in the reference environment, sorted by name.
func pythonDists(env pyEnv) ([]pyDist, error) {
	pkgs, err := parseLock(env.lock)
	if err != nil {
		return nil, err
	}
	inv, err := parseInventory(env.inventory)
	if err != nil {
		return nil, err
	}
	reach := runtimeClosure(pkgs)
	if len(reach) == 0 {
		return nil, fmt.Errorf("%s: no runtime dependencies found (lock layout changed?)", env.lock)
	}
	var out []pyDist
	for _, p := range pkgs {
		n := normName(p.name)
		d, installed := inv[n+"@"+p.version]
		if p.root || !reach[n] || !installed {
			continue
		}
		d.name = n
		d.url, d.sha256 = p.url, p.sha256
		if d.url == "" {
			// No sdist: record a pure-Python wheel if there is one, otherwise
			// the only wheel; platform wheels are chosen at install time.
			for _, w := range p.wheels {
				if strings.HasSuffix(w[0], "-none-any.whl") || len(p.wheels) == 1 {
					d.url, d.sha256 = w[0], w[1]
					break
				}
			}
		}
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out, nil
}
