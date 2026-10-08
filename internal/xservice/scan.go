package xservice

import (
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/akynte/boundedcode/internal/policy"
)

// ScanOptions bound a repository scan.
type ScanOptions struct {
	MaxFiles int // default 200000
}

var skipDirNames = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, ".venv": true, "venv": true, "dist": true, "build": true,
	"target": true, ".next": true, ".terraform": true, "testdata": true, "__pycache__": true, ".idea": true,
	".codebase-memory": true, "coverage": true,
}

// Scan analyzes one repository and returns its endpoints, sorted.
func Scan(repo, root string, opt ScanOptions) ([]Endpoint, []Diagnostic, error) {
	if opt.MaxFiles == 0 {
		opt.MaxFiles = 200000
	}
	var goFiles, jsFiles, yamlFiles, dockerFiles, envTemplates, tfFiles []string
	var protoFiles, sqlFiles, jsonFiles, xmlFiles, prismaFiles, srcFiles []string
	n := 0
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // unreadable entries are skipped
		}
		if d.IsDir() {
			if p != root && (skipDirNames[d.Name()] || strings.HasPrefix(d.Name(), ".") && d.Name() != ".github") {
				return filepath.SkipDir
			}
			return nil
		}
		// Symlinks are never followed: task worktrees are agent-written, and a
		// link could point the host at files the sandbox hides.
		if !d.Type().IsRegular() {
			return nil
		}
		n++
		if n > opt.MaxFiles {
			return filepath.SkipAll
		}
		rel, _ := filepath.Rel(root, p)
		if policy.IsSecretPath(rel) {
			return nil
		}
		name := strings.ToLower(d.Name())
		switch {
		case strings.HasSuffix(name, ".go"):
			goFiles = append(goFiles, rel)
		case strings.HasSuffix(name, ".d.ts"):
		case strings.HasSuffix(name, ".ts"), strings.HasSuffix(name, ".tsx"), strings.HasSuffix(name, ".js"),
			strings.HasSuffix(name, ".jsx"), strings.HasSuffix(name, ".mjs"), strings.HasSuffix(name, ".cjs"):
			if !strings.HasSuffix(name, ".min.js") && !strings.Contains(name, ".test.") && !strings.Contains(name, ".spec.") {
				jsFiles = append(jsFiles, rel)
			}
		case strings.HasSuffix(name, ".yaml"), strings.HasSuffix(name, ".yml"):
			yamlFiles = append(yamlFiles, rel)
		case name == "dockerfile" || strings.HasSuffix(name, ".dockerfile") || strings.HasPrefix(name, "dockerfile."):
			dockerFiles = append(dockerFiles, rel)
		case name == ".env.example" || name == ".env.sample" || name == ".env.template" || name == "env.example":
			envTemplates = append(envTemplates, rel)
		case strings.HasSuffix(name, ".tf"):
			tfFiles = append(tfFiles, rel)
		case strings.HasSuffix(name, ".proto"):
			protoFiles = append(protoFiles, rel)
		case strings.HasSuffix(name, ".sql"):
			sqlFiles = append(sqlFiles, rel)
		case strings.HasSuffix(name, ".json"):
			jsonFiles = append(jsonFiles, rel)
		case strings.HasSuffix(name, ".xml"):
			xmlFiles = append(xmlFiles, rel)
		case strings.HasSuffix(name, ".prisma"):
			prismaFiles = append(prismaFiles, rel)
		case sourceLang(name) != "":
			srcFiles = append(srcFiles, rel)
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	var eps []Endpoint
	var diags []Diagnostic
	e, d := analyzeGo(repo, root, goFiles)
	eps, diags = append(eps, e...), append(diags, d...)
	e, d = analyzeJS(repo, root, jsFiles)
	eps, diags = append(eps, e...), append(diags, d...)
	e, d = analyzeYAML(repo, root, yamlFiles)
	eps, diags = append(eps, e...), append(diags, d...)
	eps = append(eps, analyzeDockerfile(repo, root, dockerFiles)...)
	eps = append(eps, analyzeEnvTemplate(repo, root, envTemplates)...)
	e, d = analyzeTerraform(repo, root, tfFiles)
	eps, diags = append(eps, e...), append(diags, d...)
	e, d = analyzeProto(repo, root, protoFiles)
	eps, diags = append(eps, e...), append(diags, d...)
	e, d = analyzeOpenAPIJSON(repo, root, jsonFiles)
	eps, diags = append(eps, e...), append(diags, d...)
	eps = append(eps, analyzeSQLFiles(repo, root, sqlFiles)...)
	eps = append(eps, analyzeXML(repo, root, xmlFiles)...)
	e, models := analyzePrisma(repo, root, prismaFiles)
	eps = append(eps, e...)
	eps = append(eps, analyzeSources(repo, root, srcFiles, sourceLang)...)
	eps = append(eps, analyzeSources(repo, root, jsFiles, func(string) srcLang { return langJS })...)
	resolvePrisma(eps, models)
	SortEndpoints(eps)
	return dedupeEndpoints(eps), diags, nil
}

func dedupeEndpoints(es []Endpoint) []Endpoint {
	seen := map[string]bool{}
	out := es[:0]
	for _, e := range es {
		k := string(e.Kind) + "|" + e.Where() + "|" + e.Key() + "|" + e.Ref
		if !seen[k] {
			seen[k] = true
			out = append(out, e)
		}
	}
	return out
}
