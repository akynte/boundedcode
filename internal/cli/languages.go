package cli

import (
	"regexp"

	"github.com/akynte/boundedcode/internal/config"
	"github.com/akynte/boundedcode/internal/ecosystem"
	"github.com/akynte/boundedcode/internal/lang"
)

// registerLanguages adds the user's own languages and build systems to the
// built-in ones (config.Validate has checked them).
func registerLanguages(cfg *config.Config) {
	for _, l := range cfg.Languages {
		d := lang.Language{ID: l.ID, Name: l.Name, Kind: lang.KindProgramming, Extensions: l.Extensions, Filenames: l.Filenames, Shebangs: l.Shebangs}
		if d.Name == "" {
			d.Name = l.ID
		}
		for _, p := range l.Definitions {
			if re, err := regexp.Compile(p); err == nil {
				d.Definitions = append(d.Definitions, re)
			}
		}
		for _, p := range l.Tests {
			if re, err := regexp.Compile(p); err == nil {
				d.Tests = append(d.Tests, re)
			}
		}
		if c := l.Check; c != nil {
			what := c.What
			if what == "" {
				what = c.Tools[0]
			}
			d.Check = &lang.Check{What: what, Tools: c.Tools, Args: c.Args, Root: c.Root}
		}
		for _, list := range []struct {
			bins []string
			dst  *[]lang.Tool
		}{{l.Format, &d.Format}, {l.Lint, &d.Lint}, {l.LSP, &d.LSP}} {
			for _, b := range list.bins {
				*list.dst = append(*list.dst, lang.Tool{Name: b, Bin: b})
			}
		}
		lang.Register(d)
	}
	for _, b := range cfg.BuildSystems {
		kind := b.Kind
		if kind == "" {
			kind = b.ID + " project"
		}
		ecosystem.RegisterBuildSystem(b.ID, kind, b.Markers, b.Languages, b.Tool, b.Build, b.Test)
	}
}
