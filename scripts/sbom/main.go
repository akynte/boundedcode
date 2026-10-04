// Command sbom writes an SPDX 2.3 JSON SBOM for the boundedcode binary: the
// main module plus every Go module linked into ./cmd/..., with licenses
// classified by github.com/google/licensecheck. Pinned external runtime
// components (llama.cpp, OpenHands, codebase-memory-mcp, gitleaks, Serena)
// are not part of the binary; they are listed as runtime or optional
// dependencies with their exact pins (see THIRD_PARTY_NOTICES.md). The Python
// distributions of the adapter and Serena environments are listed from their
// uv.lock files (version, artifact URL and sha256; see pylock.go).
package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/licensecheck"

	"github.com/akynte/boundedcode/internal/repointel/serena"
)

// external is a pinned runtime component invoked as a separate process.
type external struct {
	name, version, location, license, purl, rel, sha256, source string
}

// externals lists pinned runtime components. Pins match
// docs/licensing/upstream-license-matrix.md; scripts/serenaguard checks Serena.
func externals() []external {
	return []external{
		{"llama.cpp", "v0.5.0", "git+https://github.com/ggml-org/llama.cpp@7fe450e19305b828c199d602c23a8337aaa1f03b", "MIT", "pkg:github/ggml-org/llama.cpp@v0.5.0", "RUNTIME_DEPENDENCY_OF", "", ""},
		{"openhands-sdk", "1.51.0", "git+https://github.com/OpenHands/software-agent-sdk@a955aa5d3188d4b0a44ad7eb4e5c4bba6e6238d9", "MIT", "pkg:pypi/openhands-sdk@1.51.0", "RUNTIME_DEPENDENCY_OF", "", ""},
		{"openhands-tools", "1.51.0", "git+https://github.com/OpenHands/software-agent-sdk@a955aa5d3188d4b0a44ad7eb4e5c4bba6e6238d9", "MIT", "pkg:pypi/openhands-tools@1.51.0", "RUNTIME_DEPENDENCY_OF", "", ""},
		{"codebase-memory-mcp", "v0.11.0", "git+https://github.com/DeusData/codebase-memory-mcp@8972ea69c6ad94b1ef1d4ffbf0a92d78d2db1798", "MIT", "pkg:github/DeusData/codebase-memory-mcp@v0.11.0", "RUNTIME_DEPENDENCY_OF", "", ""},
		{"gitleaks", "v8.30.1", "git+https://github.com/gitleaks/gitleaks@83d9cd684c87d95d656c1458ef04895a7f1cbd8e", "MIT", "pkg:github/gitleaks/gitleaks@v8.30.1", "OPTIONAL_DEPENDENCY_OF", "", ""},
		// Installed by `serena setup` from PyPI with the locked hash; the wheel
		// was verified byte-identical to the tagged source (ADR-0008).
		{serena.PackageName, serena.RequiredVersion,
			"https://files.pythonhosted.org/packages/py3/s/serena-agent/serena_agent-" + serena.RequiredVersion + "-py3-none-any.whl",
			serena.ExpectedLicense, "pkg:pypi/" + serena.PackageName + "@" + serena.RequiredVersion, "OPTIONAL_DEPENDENCY_OF", serenaWheelSHA256(),
			"built from git+https://github.com/oraios/serena@" + serena.PinnedCommit + " (tag v" + serena.RequiredVersion + ")"},
	}
}

// serenaWheelSHA256 reads the pinned wheel hash from the embedded lock file.
func serenaWheelSHA256() string {
	b, err := os.ReadFile(filepath.Join("configs", "serena", "uv.lock"))
	if err != nil {
		fatal(err)
	}
	wheel := serena.PackageName
	wheel = strings.ReplaceAll(wheel, "-", "_") + "-" + serena.RequiredVersion + "-py3-none-any.whl"
	for line := range strings.SplitSeq(string(b), "\n") {
		if i := strings.Index(line, wheel+`", hash = "sha256:`); i >= 0 {
			rest := line[i+len(wheel+`", hash = "sha256:`):]
			if j := strings.IndexByte(rest, '"'); j == 64 {
				return rest[:j]
			}
		}
	}
	fatal(fmt.Errorf("no hash for %s in configs/serena/uv.lock", wheel))
	return ""
}

type pkg struct {
	SPDXID           string   `json:"SPDXID"`
	Name             string   `json:"name"`
	VersionInfo      string   `json:"versionInfo"`
	DownloadLocation string   `json:"downloadLocation"`
	FilesAnalyzed    bool     `json:"filesAnalyzed"`
	LicenseConcluded string   `json:"licenseConcluded"`
	LicenseDeclared  string   `json:"licenseDeclared"`
	CopyrightText    string   `json:"copyrightText"`
	ExternalRefs     []extRef `json:"externalRefs,omitempty"`
	Checksums        []sum    `json:"checksums,omitempty"`
	SourceInfo       string   `json:"sourceInfo,omitempty"`
	Comment          string   `json:"comment,omitempty"`
}

type extRef struct {
	ReferenceCategory string `json:"referenceCategory"`
	ReferenceType     string `json:"referenceType"`
	ReferenceLocator  string `json:"referenceLocator"`
}

type sum struct {
	Algorithm     string `json:"algorithm"`
	ChecksumValue string `json:"checksumValue"`
}

type rel struct {
	SPDXElementID      string `json:"spdxElementId"`
	RelationshipType   string `json:"relationshipType"`
	RelatedSPDXElement string `json:"relatedSpdxElement"`
}

func main() {
	out := flag.String("o", "SBOM.spdx.json", "output file")
	version := flag.String("version", "dev", "boundedcode version")
	binary := flag.String("binary", "", "optional built binary to checksum")
	flag.Parse()
	ctx := context.Background()
	cmd := exec.CommandContext(ctx, "go", "list", "-deps", "-f", "{{with .Module}}{{if not .Main}}{{.Path}} {{.Version}} {{.Dir}}{{end}}{{end}}", "./cmd/...")
	raw, err := cmd.Output()
	if err != nil {
		fatal(err)
	}
	mods := map[string][2]string{}
	sc := bufio.NewScanner(bytes.NewReader(raw))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 3 {
			mods[f[0]] = [2]string{f[1], f[2]}
		}
	}
	names := make([]string, 0, len(mods))
	for n := range mods {
		names = append(names, n)
	}
	sort.Strings(names)
	root := pkg{SPDXID: "SPDXRef-Package-boundedcode", Name: "boundedcode", VersionInfo: *version,
		DownloadLocation: "NOASSERTION", LicenseConcluded: "Apache-2.0", LicenseDeclared: "Apache-2.0",
		CopyrightText: "Copyright 2026 The BoundedCode Authors"}
	if *binary != "" {
		b, err := os.ReadFile(*binary)
		if err != nil {
			fatal(err)
		}
		h := sha256.Sum256(b)
		root.Checksums = []sum{{"SHA256", hex.EncodeToString(h[:])}}
	}
	pkgs := []pkg{root}
	rels := []rel{{"SPDXRef-DOCUMENT", "DESCRIBES", root.SPDXID}}
	for i, n := range names {
		v, dir := mods[n][0], mods[n][1]
		lic := classify(dir)
		id := fmt.Sprintf("SPDXRef-Package-go-%d", i)
		pkgs = append(pkgs, pkg{SPDXID: id, Name: n, VersionInfo: v, DownloadLocation: "https://proxy.golang.org/" + strings.ToLower(n) + "/@v/" + v + ".zip",
			LicenseConcluded: lic, LicenseDeclared: lic, CopyrightText: "NOASSERTION",
			ExternalRefs: []extRef{{"PACKAGE-MANAGER", "purl", "pkg:golang/" + n + "@" + v}}})
		rels = append(rels, rel{root.SPDXID, "DEPENDS_ON", id})
	}
	covered := map[string]bool{}
	for i, e := range externals() {
		covered[e.name] = true
		id := fmt.Sprintf("SPDXRef-Package-external-%d", i)
		p := pkg{SPDXID: id, Name: e.name, VersionInfo: e.version, DownloadLocation: e.location,
			LicenseConcluded: e.license, LicenseDeclared: e.license, CopyrightText: "NOASSERTION",
			ExternalRefs: []extRef{{"PACKAGE-MANAGER", "purl", e.purl}}}
		if e.sha256 != "" {
			p.Checksums = []sum{{"SHA256", e.sha256}}
		}
		p.SourceInfo = e.source
		pkgs = append(pkgs, p)
		rels = append(rels, rel{id, e.rel, root.SPDXID})
	}
	for _, env := range pyEnvs() {
		dists, err := pythonDists(env)
		if err != nil {
			fatal(err)
		}
		for i, d := range dists {
			if covered[d.name] {
				continue
			}
			id := fmt.Sprintf("SPDXRef-Package-py-%s-%d", env.key, i)
			p := pkg{SPDXID: id, Name: d.name, VersionInfo: d.version, DownloadLocation: "NOASSERTION",
				LicenseConcluded: "NOASSERTION", LicenseDeclared: "NOASSERTION", CopyrightText: "NOASSERTION",
				ExternalRefs: []extRef{{"PACKAGE-MANAGER", "purl", "pkg:pypi/" + d.name + "@" + d.version}},
				SourceInfo:   "locked in " + env.lock,
				Comment:      fmt.Sprintf("license metadata: %s (scripts/pylicensecheck.py verdict: %s; see %s)", d.license, d.verdict, env.inventory)}
			if d.url != "" {
				p.DownloadLocation = d.url
				p.Checksums = []sum{{"SHA256", d.sha256}}
			}
			pkgs = append(pkgs, p)
			rels = append(rels, rel{id, env.rel, root.SPDXID})
		}
	}
	doc := map[string]any{
		"spdxVersion": "SPDX-2.3", "dataLicense": "CC0-1.0", "SPDXID": "SPDXRef-DOCUMENT",
		"name": "boundedcode-" + *version, "documentNamespace": "https://spdx.org/spdxdocs/boundedcode-" + *version + "-" + time.Now().UTC().Format("20060102T150405Z"),
		"creationInfo": map[string]any{"created": time.Now().UTC().Format(time.RFC3339), "creators": []string{"Tool: boundedcode-scripts-sbom"}},
		"packages":     pkgs, "relationships": rels,
	}
	b, _ := json.MarshalIndent(doc, "", "  ")
	if err := os.WriteFile(*out, append(b, '\n'), 0o644); err != nil {
		fatal(err)
	}
	fmt.Printf("wrote %s (%d packages)\n", *out, len(pkgs))
}

func classify(dir string) string {
	entries, _ := os.ReadDir(dir)
	ids := map[string]bool{}
	for _, e := range entries {
		n := strings.ToUpper(e.Name())
		isLicense := strings.HasPrefix(n, "LICENSE") || strings.HasPrefix(n, "COPYING")
		if e.IsDir() || !isLicense || strings.Contains(n, "3RD-PARTY") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		for _, m := range licensecheck.Scan(b).Match {
			ids[m.ID] = true
		}
	}
	if len(ids) == 0 {
		return "NOASSERTION"
	}
	var l []string
	for id := range ids {
		l = append(l, id)
	}
	sort.Strings(l)
	return strings.Join(l, " AND ")
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
