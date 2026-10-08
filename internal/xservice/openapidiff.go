package xservice

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

// OpenAPI support for the cross-repository compatibility gate
// (internal/compat): a digest of one operation, so that a spec change can
// be attributed to the operations it actually changes, and a "knock-out"
// that removes one operation, so that a check can be shown to depend on it.

// OpenAPIOp is one operation found in a spec.
type OpenAPIOp struct {
	Method, Path string // upper case; normalized full path as in Endpoint
	Line         int
	Digest       string // of the operation, its path-level parameters and every local $ref it reaches
}

// OpenAPIOperations lists the operations of a spec file's content (nil when
// it is not a spec).
func OpenAPIOperations(src []byte) []OpenAPIOp {
	var doc yaml.Node
	if err := yaml.Unmarshal(src, &doc); err != nil {
		return nil
	}
	if _, ok := isOpenAPI(&doc); !ok {
		return nil
	}
	root := doc.Content[0]
	var out []OpenAPIOp
	forEachOperation(root, func(np, method string, item, op *keyVal) {
		refs := map[string]bool{}
		collectRefs(op.val, refs)
		if params := mapGet(item.val, "parameters"); params != nil {
			collectRefs(params, refs)
		}
		for changed := true; changed; { // the closure of local references
			changed = false
			for r := range refs {
				if n := resolveLocalRef(root, r); n != nil {
					before := len(refs)
					collectRefs(n, refs)
					changed = changed || len(refs) != before
				}
			}
		}
		h := sha256.New()
		h.Write(canonical(op.val))
		h.Write(canonical(mapGet(item.val, "parameters")))
		names := make([]string, 0, len(refs))
		for r := range refs {
			names = append(names, r)
		}
		sort.Strings(names)
		for _, r := range names {
			h.Write([]byte(r))
			h.Write(canonical(resolveLocalRef(root, r)))
		}
		out = append(out, OpenAPIOp{Method: method, Path: np, Line: op.key.Line, Digest: hex.EncodeToString(h.Sum(nil))[:16]})
	})
	return out
}

// forEachOperation calls fn for every operation with its normalized full
// paths (one call per base path).
func forEachOperation(root *yaml.Node, fn func(np, method string, item, op *keyVal)) {
	paths := mapGet(root, "paths")
	if paths == nil || paths.Kind != yaml.MappingNode {
		return
	}
	bases := openAPIBases(root)
	for i := 0; i+1 < len(paths.Content); i += 2 {
		item := &keyVal{paths.Content[i], paths.Content[i+1]}
		if item.val.Kind != yaml.MappingNode {
			continue
		}
		pathBases := bases
		if s := mapGet(item.val, "servers"); s != nil {
			pathBases = serverPaths(s)
		}
		for _, m := range openAPIMethods {
			op := mapGetNode(item.val, m)
			if op == nil {
				continue
			}
			for _, base := range pathBases {
				if np := NormalizePath(joinPath(base, item.key.Value)); np != "" {
					fn(np, strings.ToUpper(m), item, op)
				}
			}
		}
	}
}

func collectRefs(n *yaml.Node, refs map[string]bool) {
	if n == nil {
		return
	}
	if n.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(n.Content); i += 2 {
			if n.Content[i].Value == "$ref" && n.Content[i+1].Kind == yaml.ScalarNode && strings.HasPrefix(n.Content[i+1].Value, "#/") {
				refs[n.Content[i+1].Value] = true
			}
		}
	}
	for _, c := range n.Content {
		collectRefs(c, refs)
	}
}

// resolveLocalRef follows a "#/a/b" JSON pointer within the document.
func resolveLocalRef(root *yaml.Node, ref string) *yaml.Node {
	n := root
	for _, part := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
		part = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
		if n = mapGet(n, part); n == nil {
			return nil
		}
	}
	return n
}

// canonical encodes a node independently of formatting, comments and key
// order.
func canonical(n *yaml.Node) []byte {
	if n == nil {
		return []byte("null")
	}
	var v any
	if err := n.Decode(&v); err != nil {
		return []byte(err.Error())
	}
	b, err := json.Marshal(jsonable(v))
	if err != nil {
		return []byte(err.Error())
	}
	return b
}

// jsonable converts YAML-decoded values (maps with non-string keys) for
// encoding/json.
func jsonable(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			x[k] = jsonable(e)
		}
		return x
	case map[any]any:
		out := map[string]any{}
		for k, e := range x {
			b, _ := json.Marshal(k)
			out[strings.Trim(string(b), `"`)] = jsonable(e)
		}
		return out
	case []any:
		for i, e := range x {
			x[i] = jsonable(e)
		}
		return x
	}
	return v
}

// ErrNoOperation is returned by OpenAPIKnockOut when the spec does not
// declare the operation.
var ErrNoOperation = errors.New("operation not found in the specification")

// OpenAPIKnockOut returns the spec with one operation removed (and its path
// item, when that was its only operation). JSON files stay JSON. Comments
// and formatting are not preserved; the result is only used for a check
// run, never written to a repository.
func OpenAPIKnockOut(src []byte, file, method, path string) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(src, &doc); err != nil {
		return nil, err
	}
	if _, ok := isOpenAPI(&doc); !ok {
		return nil, ErrNoOperation
	}
	root := doc.Content[0]
	paths := mapGet(root, "paths")
	found := false
	forEachOperation(root, func(np, m string, item, op *keyVal) {
		if found || np != path || m != method {
			return
		}
		found = true
		item.val.Content = removeKey(item.val.Content, op.key)
		hasOps := false
		for _, mm := range openAPIMethods {
			hasOps = hasOps || mapGet(item.val, mm) != nil
		}
		if !hasOps {
			paths.Content = removeKey(paths.Content, item.key)
		}
	})
	if !found {
		return nil, ErrNoOperation
	}
	if strings.EqualFold(filepath.Ext(file), ".json") {
		var v any
		if err := root.Decode(&v); err != nil {
			return nil, err
		}
		return json.MarshalIndent(jsonable(v), "", "  ")
	}
	return yaml.Marshal(&doc)
}

func removeKey(content []*yaml.Node, key *yaml.Node) []*yaml.Node {
	for i := 0; i+1 < len(content); i += 2 {
		if content[i] == key {
			return append(content[:i:i], content[i+2:]...)
		}
	}
	return content
}
