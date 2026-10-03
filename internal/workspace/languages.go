package workspace

import (
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
)

var extLang = map[string]string{
	".go": "go", ".ts": "typescript", ".tsx": "typescript", ".js": "javascript", ".jsx": "javascript",
	".py": "python", ".java": "java", ".kt": "kotlin", ".rs": "rust", ".rb": "ruby", ".cs": "csharp",
	".sql": "sql", ".proto": "protobuf", ".tf": "terraform", ".php": "php", ".c": "c", ".cpp": "cpp", ".h": "c",
}

var skipDirs = map[string]bool{".git": true, "node_modules": true, "vendor": true, ".venv": true, "dist": true, "build": true, "target": true}

// DetectLanguages returns languages present in root (bounded walk) plus
// infrastructure markers (dockerfile, helm, kubernetes).
func DetectLanguages(root string) []string {
	found := map[string]bool{}
	n := 0
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // unreadable entries are skipped
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		n++
		if n > 50000 {
			return filepath.SkipAll
		}
		name := strings.ToLower(d.Name())
		switch {
		case name == "dockerfile" || strings.HasSuffix(name, ".dockerfile"):
			found["docker"] = true
		case name == "chart.yaml":
			found["helm"] = true
		case name == "kustomization.yaml":
			found["kustomize"] = true
		}
		if l, ok := extLang[filepath.Ext(name)]; ok {
			found[l] = true
		}
		return nil
	})
	out := make([]string, 0, len(found))
	for l := range found {
		out = append(out, l)
	}
	sort.Strings(out)
	return out
}
