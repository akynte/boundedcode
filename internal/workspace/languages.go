package workspace

import (
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	"github.com/akynte/boundedcode/internal/lang"
)

// DetectLanguages returns the languages present in root (internal/lang
// IDs: code, SQL, protobuf and infrastructure; not prose or data formats),
// plus infrastructure markers (docker, helm, kustomize).
func DetectLanguages(root string) []string {
	found := map[string]bool{}
	counts, _, _ := lang.Census(root, lang.CensusOptions{})
	for _, c := range counts {
		switch c.Language.Kind {
		case lang.KindProgramming, lang.KindInfra:
			found[c.Language.ID] = true
		case lang.KindData:
			if c.Language.ID == "sql" || c.Language.ID == "protobuf" || c.Language.ID == "graphql" {
				found[c.Language.ID] = true
			}
		}
	}
	delete(found, "make")
	delete(found, "cmake")
	if found["dockerfile"] {
		delete(found, "dockerfile")
		found["docker"] = true
	}
	n := 0
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // unreadable entries are skipped
		}
		if d.IsDir() {
			if p != root && lang.SkipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if n++; n > 50000 {
			return filepath.SkipAll
		}
		switch strings.ToLower(d.Name()) {
		case "chart.yaml":
			found["helm"] = true
		case "kustomization.yaml":
			found["kustomize"] = true
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
