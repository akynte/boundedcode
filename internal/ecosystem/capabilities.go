package ecosystem

import (
	"fmt"
	"strings"

	"github.com/akynte/boundedcode/internal/lang"
)

// Generic lists what works for every text file, in any language, with no
// tool installed: the agent's floor.
var Generic = []string{"read and outline", "edit and write", "search (grep, glob)", "symbol search by definition patterns, or by name",
	"shell commands", "git"}

// LanguageReport is one language found in a repository.
type LanguageReport struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Kind         string            `json:"kind"`
	Files        int               `json:"files"`
	Tests        int               `json:"tests"`
	Capabilities []lang.Capability `json:"capabilities"`
	Note         string            `json:"note,omitempty"`
}

// ProjectReport is one project with what can be run for it here.
type ProjectReport struct {
	Project
	BuildStatus string `json:"build_status"` // lang.Available, lang.MissingTool, lang.Unsupported
	TestStatus  string `json:"test_status"`
}

// CapabilityReport is what BoundedCode can do in a repository: for every
// language found, which checks, formatters, linters and language servers
// are installed; for every project (nested ones too), its build and test
// commands and whether their tool is installed.
type CapabilityReport struct {
	Languages []LanguageReport `json:"languages"`
	Unknown   int              `json:"unknown_files"`
	Projects  []ProjectReport  `json:"projects"`
	More      int              `json:"more_projects,omitempty"`
	CI        []string         `json:"ci,omitempty"`
	Truncated bool             `json:"truncated,omitempty"`
	Generic   []string         `json:"generic"`
}

// Capabilities reports what BoundedCode can do in the repository at root.
// Tools are looked up on the host, never in the repository.
func Capabilities(root string, f *lang.Finder, opt Options) CapabilityReport {
	if f == nil {
		f = &lang.Finder{Exclude: root}
	}
	if opt.LookPath == nil {
		opt.LookPath = func(bin string) (string, error) {
			if p := f.Find(bin); p != "" {
				return p, nil
			}
			return "", fmt.Errorf("%s not found", bin)
		}
	}
	counts, unknown, truncated := lang.Census(root, lang.CensusOptions{})
	rep := CapabilityReport{Unknown: unknown, Truncated: truncated, Generic: Generic}
	for _, c := range counts {
		l := c.Language
		rep.Languages = append(rep.Languages, LanguageReport{ID: l.ID, Name: l.Name, Kind: l.Kind, Files: c.Files, Tests: c.Tests,
			Capabilities: f.Capabilities(l), Note: l.Note})
	}
	d := Detect(root, opt)
	rep.More, rep.CI = d.More, d.CI
	rep.Truncated = rep.Truncated || d.Truncated
	for _, p := range d.Projects {
		pr := ProjectReport{Project: p, BuildStatus: lang.Available, TestStatus: lang.Available}
		for _, s := range []*string{&pr.BuildStatus, &pr.TestStatus} {
			if p.Missing {
				*s = lang.MissingTool
			}
		}
		if p.Build == "" {
			pr.BuildStatus = lang.Unsupported
		}
		if p.Test == "" {
			pr.TestStatus = lang.Unsupported
		}
		rep.Projects = append(rep.Projects, pr)
	}
	return rep
}

// String renders the report for a terminal.
func (r CapabilityReport) String() string {
	var b strings.Builder
	b.WriteString("Languages (files, tests; check, format, lint, language server):\n")
	for _, l := range r.Languages {
		fmt.Fprintf(&b, "  %-18s %5d %4d  %s\n", l.Name, l.Files, l.Tests, capLine(l.Capabilities))
		if l.Note != "" {
			fmt.Fprintf(&b, "  %-18s             %s\n", "", l.Note)
		}
	}
	if r.Unknown > 0 {
		fmt.Fprintf(&b, "  %-18s %5d        generic support only\n", "other text", r.Unknown)
	}
	b.WriteString("Projects (build; test):\n")
	if len(r.Projects) == 0 {
		b.WriteString("  none found\n")
	}
	for _, p := range r.Projects {
		dir := p.Dir
		if dir == "." {
			dir = "(root)"
		}
		fmt.Fprintf(&b, "  %-24s %s\n", dir, p.Describe())
		fmt.Fprintf(&b, "  %-24s build %s; test %s\n", "", runStatus(p.BuildStatus), runStatus(p.TestStatus))
	}
	if r.More > 0 {
		fmt.Fprintf(&b, "  … and %d more\n", r.More)
	}
	if len(r.CI) > 0 {
		b.WriteString("CI: " + strings.Join(r.CI, ", ") + "\n")
	}
	b.WriteString("Every file, any language: " + strings.Join(r.Generic, "; ") + "\n")
	if r.Truncated {
		b.WriteString("(projects are looked for in the top directory levels, languages in the first 50,000 files)\n")
	}
	return b.String()
}

// runStatus renders a project command's status.
func runStatus(s string) string {
	if s == lang.Unsupported {
		return "none known"
	}
	return s
}

func capLine(cs []lang.Capability) string {
	var parts []string
	for _, c := range cs {
		switch c.Status {
		case lang.Available:
			parts = append(parts, c.Kind+": "+c.Tool)
		case lang.MissingTool:
			parts = append(parts, c.Kind+": missing ("+c.Tool+")")
		default:
			parts = append(parts, c.Kind+": —")
		}
	}
	return strings.Join(parts, "; ")
}
