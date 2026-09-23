package judgment

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// There must be no generic serialization path into a request.
//
// The first version of State had Fact(key string, value any), which marshalled
// whatever it was handed. That made "every piece of repository data passes a
// guarded door" true of the doors and false of the building: any caller could
// serialise a slice of retrieval candidates straight past the sensitive-path
// check, the neutraliser and the accounting.
//
// This is a structural test rather than a behavioural one because the property
// is about what the API permits, not about what today's callers happen to do.
// A behavioural test passes right up until someone adds the call.
func TestStateHasNoGenericSerializationDoor(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "state.go", nil, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}

	// Methods on State or RepoItem that accept an unconstrained value are the
	// hazard. TrustedFact is the one exception, and it is exempt only because
	// it rejects every composite type at runtime — which TestTrustedFact...
	// below proves.
	allowed := map[string]bool{"TrustedFact": true}

	ast.Inspect(file, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || !ast.IsExported(fn.Name.Name) {
			return true
		}
		recv := receiverName(fn)
		if recv != "State" && recv != "RepoItem" {
			return true
		}
		for _, param := range fn.Type.Params.List {
			if !isUnconstrained(param.Type) || allowed[fn.Name.Name] {
				continue
			}
			t.Errorf("%s.%s takes an unconstrained %s parameter. Repository data must "+
				"enter through NewRepoItem, where its origin is checked; an `any` "+
				"parameter is a door around that.",
				recv, fn.Name.Name, exprString(param.Type))
		}
		return true
	})
}

// TrustedFact is the one door taking `any`, and it earns that only by
// refusing everything that could carry a collection of repository records.
func TestTrustedFactRefusesCompositeValues(t *testing.T) {
	type candidate struct {
		Path string
	}
	for name, value := range map[string]any{
		"a struct":          candidate{Path: "internal/secret.go"},
		"a slice":           []candidate{{Path: "internal/secret.go"}},
		"a map":             map[string]string{"path": "internal/secret.go"},
		"a pointer":         &candidate{Path: "internal/secret.go"},
		"a slice of string": []string{"internal/secret.go"},
		"raw json":          []byte(`{"path":"internal/secret.go"}`),
	} {
		t.Run(name, func(t *testing.T) {
			st := NewState(RedactStrict)
			if err := st.TrustedFact("candidates", value); err == nil {
				t.Fatalf("%s was accepted; a collection of repository records could be "+
					"serialised past every check in state.go", name)
			}
			if !st.Empty() {
				t.Fatal("a refused value was stored anyway")
			}
		})
	}
	// Scalars are what the door is for.
	for name, value := range map[string]any{
		"a string": "INTAKE", "an int": 7, "a bool": true, "a float": 1.5,
	} {
		st := NewState(RedactStrict)
		if err := st.TrustedFact("k", value); err != nil {
			t.Errorf("%s was refused: %v", name, err)
		}
	}
}

// A RepoItem cannot be built without an origin that passed the egress check,
// and there is no way to hand one a field that skipped it.
func TestRepoItemCannotCarryAnIneligibleOrigin(t *testing.T) {
	st := NewState(RedactRepoText)
	for _, origin := range []string{
		"deploy/.env", "certs/server.key", "internal/config/secrets.go",
		".ssh/id_rsa", ".npmrc", ".git/config", "",
	} {
		if _, err := st.NewRepoItem("c0", origin); err == nil {
			t.Errorf("a record was opened for %q", origin)
		}
	}
	if st.RepoMetadataFields() != 0 || st.RepoTextFields() != 0 {
		t.Fatal("a refused record was accounted for as sent")
	}
}

// Metadata is permitted in strict mode; source is not, and the refusal is an
// error rather than a silently dropped field.
func TestRepoItemTextNeedsRepoTextMode(t *testing.T) {
	st := NewState(RedactStrict)
	item, err := st.NewRepoItem("c0", "internal/task/runner.go")
	if err != nil {
		t.Fatal(err)
	}
	item.Meta("symbol", "Run").Lines(10, 20)
	if err := item.Text("signature", "func Run() error", st); err == nil {
		t.Fatal("repository source was accepted in strict mode")
	}
	if err := st.AddItems("candidates", []*RepoItem{item}); err != nil {
		t.Fatal(err)
	}
	body, err := st.canonical()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(body, "func Run() error") {
		t.Fatalf("the refused signature is in the payload: %s", body)
	}
	if !strings.Contains(body, "Run") || !strings.Contains(body, "runner.go") {
		t.Fatalf("metadata did not survive: %s", body)
	}
	if st.RepoMetadataFields() != 1 {
		t.Fatalf("metadata accounting = %d, want 1", st.RepoMetadataFields())
	}
	if st.RepoTextFields() != 0 {
		t.Fatalf("text accounting = %d after a refusal", st.RepoTextFields())
	}
}

// Duplicate record ids would make a question naming one ambiguous.
func TestAddItemsRefusesDuplicateIDs(t *testing.T) {
	st := NewState(RedactStrict)
	a, _ := st.NewRepoItem("c0", "a.go")
	b, _ := st.NewRepoItem("c0", "b.go")
	if err := st.AddItems("candidates", []*RepoItem{a, b}); err == nil {
		t.Fatal("two records share an id; a proposition naming c0 is ambiguous")
	}
}

func receiverName(fn *ast.FuncDecl) string {
	if len(fn.Recv.List) == 0 {
		return ""
	}
	return exprString(fn.Recv.List[0].Type)
}

func exprString(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.StarExpr:
		return exprString(v.X)
	case *ast.Ident:
		return v.Name
	case *ast.InterfaceType:
		if v.Methods == nil || len(v.Methods.List) == 0 {
			return "any"
		}
		return "interface{…}"
	case *ast.SelectorExpr:
		return exprString(v.X) + "." + v.Sel.Name
	case *ast.ArrayType:
		return "[]" + exprString(v.Elt)
	case *ast.MapType:
		return "map[" + exprString(v.Key) + "]" + exprString(v.Value)
	default:
		return "?"
	}
}

// isUnconstrained reports a parameter that could carry arbitrary structured
// data: `any`, or a composite of it.
func isUnconstrained(e ast.Expr) bool {
	switch v := e.(type) {
	case *ast.Ident:
		return v.Name == "any"
	case *ast.InterfaceType:
		return v.Methods == nil || len(v.Methods.List) == 0
	case *ast.ArrayType:
		return isUnconstrained(v.Elt)
	case *ast.MapType:
		return isUnconstrained(v.Value)
	default:
		return false
	}
}
