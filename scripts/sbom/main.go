// Command sbom writes an SPDX 2.3 JSON SBOM for the boundedcode binary: the
// main module plus every Go module linked into ./cmd/..., with licenses
// classified by github.com/google/licensecheck. External runtimes (llama.cpp,
// OpenHands, codebase-memory-mcp) are not part of the binary and are listed in
// THIRD_PARTY_NOTICES.md instead.
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
)

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
