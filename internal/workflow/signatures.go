package workflow

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/akynte/boundedcode/internal/graph"
	"github.com/akynte/boundedcode/internal/policy"
	"github.com/akynte/boundedcode/internal/treesitter"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"sort"
	"strings"
)

// ChangedGoSignatures compares public contracts without parameter names or
// bodies. Compiler errors are surfaced rather than guessed from text matches.
func ChangedGoSignatures(before, after []byte) ([]string, error) {
	old, err := goSignatures(before)
	if err != nil {
		return nil, err
	}
	next, err := goSignatures(after)
	if err != nil {
		return nil, err
	}
	var changed []string
	seen := map[string]bool{}
	for name, signature := range old {
		if next[name] != signature {
			if dot := strings.LastIndex(name, "."); dot >= 0 {
				name = name[dot+1:]
			}
			if !seen[name] {
				changed = append(changed, name)
				seen[name] = true
			}
		}
	}
	sort.Strings(changed)
	return changed, nil
}
func goSignatures(body []byte) (map[string]string, error) {
	out := map[string]string{}
	if len(body) == 0 {
		return out, nil
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "source.go", body, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	clearNames := func(fields *ast.FieldList) {
		if fields != nil {
			var expanded []*ast.Field
			for _, field := range fields.List {
				for n := 0; n < max(1, len(field.Names)); n++ {
					copy := *field
					copy.Names = nil
					copy.Doc = nil
					copy.Comment = nil
					expanded = append(expanded, &copy)
				}
			}
			fields.List = expanded
		}
	}
	for _, decl := range file.Decls {
		switch node := decl.(type) {
		case *ast.FuncDecl:
			if !ast.IsExported(node.Name.Name) {
				continue
			}
			clearNames(node.Type.Params)
			clearNames(node.Type.Results)
			var signature bytes.Buffer
			_ = format.Node(&signature, fset, node.Type)
			name := node.Name.Name
			if node.Recv != nil && len(node.Recv.List) > 0 {
				var receiver bytes.Buffer
				_ = format.Node(&receiver, fset, node.Recv.List[0].Type)
				name = receiver.String() + "." + name
			}
			out[name] = signature.String()
		case *ast.GenDecl:
			for _, spec := range node.Specs {
				if typ, ok := spec.(*ast.TypeSpec); ok && ast.IsExported(typ.Name.Name) {
					var signature bytes.Buffer
					_ = format.Node(&signature, fset, typ.Type)
					out[typ.Name.Name] = signature.String()
				}
			}
		}
	}
	return out, nil
}

// PlannerActionable reports a graph node a plan can be asked to account for.
//
// The impact set is the graph's answer to "what does this change reach", and
// for that purpose a file node, a directory node and a function-local
// variable are all legitimate answers. As *obligations* they are not: an
// obligation asks a planner to say what happens to a declaration, and the
// pytest reproduction demanded one for symbols literally named `209`, `11`,
// `13`, `5`, `167`, `213`, `6`, `168` and `215` — SCIP `local N` descriptors
// whose short name is the number — plus the file `fixtures.py` itself. Ten
// of sixteen obligations were unanswerable, so the plan could not be
// accepted however well the model reasoned.
//
// The rule is about the node's kind and identity, never about which
// repository it came from.
func PlannerActionable(n graph.Node) bool {
	// Without a file and a line there is no declaration to speak about.
	if n.Path == "" || n.StartLine <= 0 {
		return false
	}
	switch n.Kind {
	case graph.KindFunction, graph.KindMethod, graph.KindClass, graph.KindInterface,
		graph.KindType, graph.KindField, graph.KindConstant:
		// A stable declaration somebody can name.
	default:
		// Files, directories, packages, modules, plain variables: real parts
		// of the graph, and not things a plan resolves one by one.
		return false
	}
	return !anonymousSymbol(n)
}

// anonymousSymbol reports a declaration with no name a person could use.
//
// SCIP encodes a function-local binding as `local 209`, and the descriptor
// parser faithfully renders its name as "209". Faithful and useless: nothing
// in the repository is called that, and a planner asked about it can only
// guess. A numeric name is the signal, and the fully-qualified form confirms
// it without having to trust either alone.
func anonymousSymbol(n graph.Node) bool {
	if strings.Contains(n.FQN, "::local ") || strings.HasPrefix(n.FQN, "local ") {
		return true
	}
	name := strings.TrimSpace(n.Name)
	if name == "" {
		return true
	}
	for _, r := range name {
		if r < '0' || r > '9' {
			return false
		}
	}
	// Every character was a digit.
	return true
}

// ActionableConsumers filters an impact set down to what a plan can answer
// for, and reports how many were dropped so the reduction is visible rather
// than silent.
func ActionableConsumers(impact *graph.Impact) (kept []graph.Consumer, dropped int) {
	if impact == nil {
		return nil, 0
	}
	for _, c := range impact.Consumers {
		if PlannerActionable(c.Node) {
			kept = append(kept, c)
			continue
		}
		dropped++
	}
	return kept, dropped
}

// NamesConsumer reports whether an obligation's symbol names this consumer:
// its short name, its fully-qualified name, or any tail of that name that
// starts at a dot or a slash — `BulkDiscount.Discount`, or the package-
// qualified `pricing.BulkDiscount.Discount`, whose package name follows the
// last slash of a Go import path.
//
// The tail is how a person writes a method. The recorded failure: a plan
// answered for `BulkDiscount.Discount` and `LoyaltyDiscount.Discount`, the
// only unambiguous way to name two methods called Discount in one file, and
// both were refused because the rule accepted only `Discount` or the whole
// module-path FQN. The path is matched separately and exactly, so a tail
// cannot reach a declaration in another file.
func NamesConsumer(symbol string, node graph.Node) bool {
	if graph.NameMatches(symbol, node) {
		return true
	}
	// An annotated name — `New (inventory)`, recorded in a plan — names the
	// declaration before the annotation.
	_, bare, _ := graph.SplitSymbol(symbol)
	return bare != symbol && graph.NameMatches(bare, node)
}

// ValidateObligations requires an explicit disposition for every discovered
// consumer. A planner may explain compatibility but cannot silently omit it.
func (p Plan) ValidateObligations(impact *graph.Impact) error {
	if impact == nil {
		return nil
	}
	for _, consumer := range impact.Consumers {
		if !PlannerActionable(consumer.Node) {
			// A file, a directory or a function-local binding is a real
			// consumer and not one a plan can resolve. See the predicate.
			continue
		}
		found := false
		for _, o := range p.Obligations {
			if o.Path != consumer.Node.Path || !NamesConsumer(o.Symbol, consumer.Node) {
				continue
			}
			if !o.Resolution.Valid() {
				continue
			}
			if o.Resolution.Action == ActionEdit && policy.Covers(p.WriteAllowlist, o.Path) {
				found = true
			}
			if o.Resolution.Action == ActionNoChange {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("unresolved impact obligation: %s in %s", consumer.Node.FQN, consumer.Node.Path)
		}
	}
	return nil
}

// ChangedSignatures reports the declarations whose signature differs between
// two versions of a file, in any language this build can parse.
//
// Go keeps go/parser: it is exact, it understands the receiver forms, and it is
// already covered by tests. Everything else goes through tree-sitter, which is
// what closes the hole this function exists to close — before it, the caller
// checked `.go` and skipped every other file, so an attempt could change a
// public Rust function or a TypeScript method and the obligations check would
// find no callers to account for, having never looked. A silent no is the worst
// answer a safety check can give.
func ChangedSignatures(file string, before, after []byte) ([]string, error) {
	if strings.HasSuffix(file, ".go") {
		return ChangedGoSignatures(before, after)
	}
	names, err := treesitter.ChangedSignatures(file, before, after)
	if errors.Is(err, treesitter.ErrUnsupported) {
		// A language with no grammar contributes no obligations. That is a
		// known limit, not a clean bill of health, and Supported reports it so
		// a caller can say which files were not examined.
		return nil, nil
	}
	return names, err
}

// SignaturesSupported reports whether a file's language can be checked for
// signature changes at all.
func SignaturesSupported(file string) bool {
	return strings.HasSuffix(file, ".go") || treesitter.Supports(file)
}
