// Command licensecheck classifies the license of every Go module linked into
// the shipped binaries and enforces the project license policy
// (docs/licensing/policy.md). It exits non-zero on a denied license or on a
// license needing manual review that is not listed in the review allowlist.
package main

import (
	"bufio"
	"bytes"
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/google/licensecheck"
)

var (
	allowed = map[string]bool{
		"Apache-2.0": true, "MIT": true, "BSD-2-Clause": true, "BSD-3-Clause": true, "ISC": true,
		"BSD-3-Clause-Clear": true, "0BSD": true, "Unlicense": true, "CC0-1.0": true,
	}
	reviewPrefixes = []string{"MPL-", "LGPL-", "EPL-", "CDDL-"}
	deniedPrefixes = []string{"GPL-", "AGPL-", "SSPL", "BUSL", "Elastic", "Commons-Clause"}
	// reviewed records modules whose review-class license was explicitly
	// approved, with the reason. Keep empty unless a review happened.
	reviewed = map[string]string{}
)

type mod struct{ path, version, dir string }

func main() {
	verbose := flag.Bool("v", false, "print every module")
	writeDir := flag.String("write", "", "copy license files of linked modules into this directory")
	flag.Parse()
	mods, err := linkedModules("./cmd/...")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	failed := false
	for _, m := range mods {
		id, cover := classify(m.dir)
		verdict := verdictFor(m.path, id)
		if *verbose || verdict != "ok" {
			fmt.Printf("%-6s %-55s %-12s %s (%.0f%% coverage)\n", verdict, m.path+"@"+m.version, id, m.dir, cover)
		}
		if verdict == "DENY" || verdict == "REVIEW" || verdict == "UNKNOWN" {
			failed = true
		}
		if *writeDir != "" {
			if err := copyLicenses(m, *writeDir); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(2)
			}
		}
	}
	if failed {
		fmt.Println("license policy violations found; see docs/licensing/policy.md")
		os.Exit(1)
	}
	fmt.Printf("license check passed for %d modules\n", len(mods))
}

func verdictFor(path, id string) string {
	if id == "" {
		return "UNKNOWN"
	}
	for _, part := range strings.Split(id, ",") {
		switch {
		case allowed[part]:
		case hasPrefix(part, deniedPrefixes):
			return "DENY"
		case hasPrefix(part, reviewPrefixes):
			if _, ok := reviewed[path]; !ok {
				return "REVIEW"
			}
		default:
			return "UNKNOWN"
		}
	}
	return "ok"
}

func hasPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// linkedModules lists non-main modules that provide packages linked into pkgs.
func linkedModules(pkgs string) ([]mod, error) {
	cmd := exec.CommandContext(context.Background(), "go", "list", "-deps", "-f", "{{with .Module}}{{if not .Main}}{{.Path}} {{.Version}} {{.Dir}}{{end}}{{end}}", pkgs)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go list: %w: %s", err, stderr.String())
	}
	seen := map[string]mod{}
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 3 {
			seen[f[0]] = mod{f[0], f[1], f[2]}
		}
	}
	mods := make([]mod, 0, len(seen))
	for _, m := range seen {
		mods = append(mods, m)
	}
	sort.Slice(mods, func(i, j int) bool { return mods[i].path < mods[j].path })
	return mods, sc.Err()
}

func isLicenseFile(name string) bool {
	name = strings.ToUpper(name)
	return strings.HasPrefix(name, "LICENSE") || strings.HasPrefix(name, "LICENCE") ||
		strings.HasPrefix(name, "COPYING") || strings.HasPrefix(name, "NOTICE")
}

// copyLicenses copies license and notice files of m into dir/<module path>@<version>/.
func copyLicenses(m mod, dir string) error {
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		return err
	}
	dst := filepath.Join(dir, strings.ReplaceAll(m.path, "/", "_")+"@"+m.version)
	for _, e := range entries {
		if e.IsDir() || !isLicenseFile(e.Name()) {
			continue
		}
		b, err := os.ReadFile(filepath.Join(m.dir, e.Name()))
		if err != nil {
			return err
		}
		if err := os.MkdirAll(dst, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dst, e.Name()), b, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// classify finds license files at the module root and returns the distinct
// SPDX ids found (comma-separated) and the best coverage percentage.
func classify(dir string) (string, float64) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", 0
	}
	ids := map[string]bool{}
	best := 0.0
	for _, e := range entries {
		name := strings.ToUpper(e.Name())
		if e.IsDir() || !isLicenseFile(name) || strings.HasPrefix(name, "NOTICE") {
			continue
		}
		// Aggregated third-party inventories (e.g. modernc.org/sqlite's
		// LICENSE-3RD-PARTY.md) describe modules that may not be linked; linked
		// modules are classified individually, so inventories are skipped.
		if strings.Contains(name, "3RD-PARTY") || strings.Contains(name, "THIRD-PARTY") || strings.Contains(name, "THIRD_PARTY") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		cov := licensecheck.Scan(b)
		if cov.Percent > best {
			best = cov.Percent
		}
		for _, m := range cov.Match {
			ids[m.ID] = true
		}
	}
	list := make([]string, 0, len(ids))
	for id := range ids {
		list = append(list, id)
	}
	sort.Strings(list)
	return strings.Join(list, ","), best
}
