package xservice

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"
)

// OpenAPI (3.x) and Swagger (2.0) specifications declare HTTP operations.
// Each operation becomes an OpenAPIOperation endpoint with the full path
// (base path from `basePath` or `servers`, with server variables at their
// defaults), so that the routes implementing it and the HTTP calls using it
// link to the spec, which is the published contract.

var openAPIMethods = []string{"get", "put", "post", "delete", "options", "head", "patch", "trace"}

// isOpenAPI reports whether a YAML/JSON document root is a spec.
func isOpenAPI(root *yaml.Node) (string, bool) {
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		root = root.Content[0]
	}
	if root.Kind != yaml.MappingNode || mapGet(root, "paths") == nil {
		return "", false
	}
	if v := mapGet(root, "openapi"); v != nil && v.Kind == yaml.ScalarNode && strings.HasPrefix(v.Value, "3") {
		return "openapi " + v.Value, true
	}
	if v := mapGet(root, "swagger"); v != nil && v.Kind == yaml.ScalarNode && strings.HasPrefix(v.Value, "2") {
		return "swagger " + v.Value, true
	}
	return "", false
}

// openAPIOperations returns the operations of a spec document.
func openAPIOperations(repo, rel string, doc *yaml.Node) ([]Endpoint, []Diagnostic) {
	version, ok := isOpenAPI(doc)
	if !ok {
		return nil, nil
	}
	root := doc
	if root.Kind == yaml.DocumentNode {
		root = root.Content[0]
	}
	var out []Endpoint
	var diags []Diagnostic
	bases := openAPIBases(root)
	paths := mapGet(root, "paths")
	if paths.Kind != yaml.MappingNode {
		return nil, nil
	}
	for i := 0; i+1 < len(paths.Content); i += 2 {
		p, item := paths.Content[i], paths.Content[i+1]
		if item.Kind != yaml.MappingNode {
			continue
		}
		if ref := mapGet(item, "$ref"); ref != nil {
			diags = append(diags, Diagnostic{File: rel, Line: p.Line, Message: "openapi: path item $ref not followed: " + ref.Value})
			continue
		}
		pathBases := bases
		if s := mapGet(item, "servers"); s != nil { // a path-level override
			pathBases = serverPaths(s)
		}
		for _, m := range openAPIMethods {
			op := mapGetNode(item, m)
			if op == nil {
				continue
			}
			sym := ""
			if id := mapGet(op.val, "operationId"); id != nil {
				sym = id.Value
			}
			for _, base := range pathBases {
				np := NormalizePath(joinPath(base, p.Value))
				if np == "" {
					continue
				}
				out = append(out, Endpoint{Kind: OpenAPIOperation, Repo: repo, File: filepath.ToSlash(rel), Line: op.key.Line,
					Method: strings.ToUpper(m), Path: np, Symbol: sym, Confidence: Exact, Detail: version})
			}
		}
	}
	return out, diags
}

type keyVal struct{ key, val *yaml.Node }

func mapGetNode(n *yaml.Node, key string) *keyVal {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return &keyVal{n.Content[i], n.Content[i+1]}
		}
	}
	return nil
}

// openAPIBases are the distinct base paths operations are served under.
func openAPIBases(root *yaml.Node) []string {
	if bp := mapGet(root, "basePath"); bp != nil && bp.Kind == yaml.ScalarNode { // Swagger 2.0
		return []string{bp.Value}
	}
	if s := mapGet(root, "servers"); s != nil {
		return serverPaths(s)
	}
	return []string{""}
}

// serverPaths are the path parts of OpenAPI 3 server URLs, with
// variables at their defaults.
func serverPaths(servers *yaml.Node) []string {
	seen := map[string]bool{}
	var out []string
	if servers.Kind == yaml.SequenceNode {
		for _, s := range servers.Content {
			u := mapGet(s, "url")
			if u == nil || u.Kind != yaml.ScalarNode {
				continue
			}
			url := u.Value
			if vars := mapGet(s, "variables"); vars != nil && vars.Kind == yaml.MappingNode {
				for i := 0; i+1 < len(vars.Content); i += 2 {
					if def := mapGet(vars.Content[i+1], "default"); def != nil {
						url = strings.ReplaceAll(url, "{"+vars.Content[i].Value+"}", def.Value)
					}
				}
			}
			p := ""
			if i := strings.Index(url, "://"); i >= 0 {
				rest := url[i+3:]
				if j := strings.IndexByte(rest, '/'); j >= 0 {
					p = rest[j:]
				}
			} else if strings.HasPrefix(url, "/") {
				p = url
			}
			p = strings.TrimRight(p, "/")
			if !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	if len(out) == 0 {
		return []string{""}
	}
	return out
}

// analyzeOpenAPIJSON reads JSON specs. Only files that mention "openapi" or
// "swagger" near the start are parsed.
func analyzeOpenAPIJSON(repo, root string, relFiles []string) ([]Endpoint, []Diagnostic) {
	var out []Endpoint
	var diags []Diagnostic
	for _, rel := range relFiles {
		if !jsonSpecHead(filepath.Join(root, rel)) {
			continue
		}
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil || len(b) > maxSpecBytes {
			continue
		}
		var doc yaml.Node
		if err := yaml.Unmarshal(b, &doc); err != nil {
			diags = append(diags, Diagnostic{File: rel, Message: "openapi json: " + err.Error()})
			continue
		}
		e, d := openAPIOperations(repo, rel, &doc)
		out, diags = append(out, e...), append(diags, d...)
	}
	return out, diags
}

// maxSpecBytes bounds the specification files read.
const maxSpecBytes = 8 << 20

// jsonSpecHead reports whether a JSON file's first 4 KiB name an OpenAPI or
// Swagger version, so that other JSON (fixtures, lock files) is not parsed.
func jsonSpecHead(p string) bool {
	f, err := os.Open(p)
	if err != nil {
		return false
	}
	defer f.Close()
	head := make([]byte, 4096)
	n, _ := io.ReadFull(f, head)
	head = head[:n]
	return bytes.Contains(head, []byte(`"openapi"`)) || bytes.Contains(head, []byte(`"swagger"`))
}
