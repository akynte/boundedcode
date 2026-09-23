// Package scipindex imports compiler-produced SCIP cross references into the
// workspace index. It retains exact occurrences alongside the shared graph.
package scipindex

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/akynte/boundedcode/internal/firewall"
	"github.com/akynte/boundedcode/internal/store"
	"github.com/akynte/boundedcode/internal/version"
	"github.com/akynte/boundedcode/internal/worktree"
	"github.com/scip-code/scip/bindings/go/scip"
	"google.golang.org/protobuf/proto"
)

type Stats struct {
	ExcludedDocuments int `json:"excluded_documents"`
	Documents         int `json:"documents"`
	Symbols           int `json:"symbols"`
	Occurrences       int `json:"occurrences"`
	Relationships     int `json:"relationships"`
}
type source struct {
	doc            *scip.Document
	hash           string
	fileID, nodeID int64
}
type symbol struct {
	info       *scip.SymbolInformation
	src        *source
	start, end int
	nodeID     int64
	kind       string
}

func scoped(file, name string) string {
	if strings.HasPrefix(name, "local ") {
		return file + "::" + name
	}
	return name
}
func kindOf(info *scip.SymbolInformation) string {
	switch strings.ToLower(info.Kind.String()) {
	case "function", "method", "class", "interface", "field", "constant", "variable", "module", "type":
		return strings.ToLower(info.Kind.String())
	case "struct", "enum", "typealias":
		return "type"
	}
	// An indexer that leaves Kind unspecified has not left the information
	// out: SCIP's symbol grammar encodes it in the descriptor suffix, and
	// that grammar is the specification rather than a convention. scip-python
	// is the case in hand — every symbol arrives UnspecifiedKind with an
	// empty display name — and reading the suffix is what turns
	// `pkg.core`/Handler#describe(). into a method called describe rather
	// than a variable whose name is the whole symbol string.
	if kind, _, ok := fromDescriptor(info.Symbol); ok {
		return kind
	}
	return "variable"
}

// fromDescriptor reads a symbol's kind and human name out of its SCIP
// descriptors, using the parser from the bindings rather than splitting the
// string here. The last descriptor names the symbol; the one before it says
// whether a term is a field on a type or a module-level constant, and whether
// a method is a method or a free function.
func fromDescriptor(symbol string) (kind, name string, ok bool) {
	parsed, err := scip.ParseSymbol(symbol)
	if err != nil || len(parsed.Descriptors) == 0 {
		return "", "", false
	}
	last := parsed.Descriptors[len(parsed.Descriptors)-1]
	var enclosing *scip.Descriptor
	if len(parsed.Descriptors) > 1 {
		enclosing = parsed.Descriptors[len(parsed.Descriptors)-2]
	}
	inType := enclosing != nil && enclosing.Suffix == scip.Descriptor_Type

	switch last.Suffix {
	case scip.Descriptor_Namespace:
		kind = "module"
	case scip.Descriptor_Type:
		kind = "class"
	case scip.Descriptor_Method:
		kind = "function"
		if inType {
			kind = "method"
		}
	case scip.Descriptor_Term:
		kind = "variable"
		if inType {
			kind = "field"
		}
	case scip.Descriptor_Parameter, scip.Descriptor_TypeParameter, scip.Descriptor_Local:
		kind = "variable"
	case scip.Descriptor_Meta:
		// The module's own marker, `__init__:`. It is the module, not a
		// variable inside one, and retrieval treats the two very differently.
		kind = "module"
		if name = last.Name; name == "__init__" && enclosing != nil {
			name = enclosing.Name
		}
		return kind, name, true
	case scip.Descriptor_Macro:
		kind = "function"
	default:
		return "", "", false
	}
	return kind, last.Name, true
}

// displayName is what a person and a symbol lookup call this symbol.
//
// Retrieval asks the graph for a name a plan mentioned — "handler",
// "Account" — so a node named with the full SCIP string is a node nothing
// will ever find. Indexers that fill DisplayName are believed; the rest have
// their name read out of the descriptor.
func displayName(info *scip.SymbolInformation, scopedName string) string {
	if info.DisplayName != "" {
		return info.DisplayName
	}
	if _, name, ok := fromDescriptor(info.Symbol); ok && name != "" {
		return name
	}
	return scopedName
}

// Import replaces the repository's previous SCIP import atomically. Files must
// already be indexed; secret paths, escaping paths and mismatched embedded
// source text are refused. The observed source hashes support invalidation.
func Import(ctx context.Context, st *store.Store, repoID, root, indexPath string) (Stats, error) {
	var stats Stats
	info, err := os.Stat(indexPath)
	if err != nil {
		return stats, err
	}
	if info.Size() > 256<<20 {
		return stats, fmt.Errorf("SCIP index exceeds 256 MiB")
	}
	body, err := os.ReadFile(indexPath)
	if err != nil {
		return stats, err
	}
	var idx scip.Index
	if err := proto.Unmarshal(body, &idx); err != nil {
		return stats, fmt.Errorf("decode SCIP: %w", err)
	}
	var sources []*source
	symbols := map[string]*symbol{}
	for _, doc := range idx.Documents {
		if err := ctx.Err(); err != nil {
			return stats, err
		}
		file := doc.RelativePath
		if path.IsAbs(file) || file == ".." || strings.HasPrefix(file, "../") {
			stats.ExcludedDocuments++
			continue
		}
		if file == "" || path.Clean(file) != file || strings.Contains(file, "\\") {
			return stats, fmt.Errorf("invalid SCIP document path %q", file)
		}
		if err := (firewall.Access{}).Check(root, file, false); err != nil {
			return stats, err
		}
		full, err := worktree.Resolve(root, file)
		if err != nil {
			return stats, err
		}
		text, err := os.ReadFile(full)
		if err != nil {
			return stats, err
		}
		if doc.Text != "" && doc.Text != string(text) {
			return stats, fmt.Errorf("SCIP source differs from checkout: %s", file)
		}
		h := sha256.Sum256(text)
		src := &source{doc: doc, hash: hex.EncodeToString(h[:])}
		if err := st.Index().SQL().QueryRowContext(ctx, `SELECT file_id FROM files WHERE workspace_id=? AND repository_id=? AND path=?`, st.ID().String(), repoID, file).Scan(&src.fileID); err != nil {
			return stats, fmt.Errorf("index source file %s before importing SCIP: %w", file, err)
		}
		if err := st.Index().SQL().QueryRowContext(ctx, `SELECT node_id FROM nodes WHERE repository_id=? AND file_id=? AND kind='file'`, repoID, src.fileID).Scan(&src.nodeID); err != nil {
			return stats, err
		}
		sources = append(sources, src)
		for _, info := range doc.Symbols {
			name := scoped(file, info.Symbol)
			if _, exists := symbols[name]; exists {
				continue
			}
			symbols[name] = &symbol{info: info, src: src, kind: kindOf(info)}
		}
		for _, occ := range doc.Occurrences {
			r, ok := occ.SourceRange()
			if !ok || r.Start.Line < 0 || r.Start.Character < 0 || r.End.Line < r.Start.Line {
				return stats, fmt.Errorf("invalid SCIP occurrence range in %s", file)
			}
			if occ.SymbolRoles&int32(scip.SymbolRole_Definition) != 0 {
				name := scoped(file, occ.Symbol)
				sym := symbols[name]
				if sym == nil {
					// A definition with no SymbolInformation beside it still
					// has its descriptors, so it is named and classified the
					// same way as one that does.
					info := &scip.SymbolInformation{Symbol: occ.Symbol}
					sym = &symbol{info: info, src: src, kind: kindOf(info)}
					symbols[name] = sym
				}
				if sym.src != src {
					continue
				}
				sym.start, sym.end = int(r.Start.Line)+1, int(r.End.Line)+1
				if enclosing, ok := occ.EnclosingSourceRange(); ok {
					sym.start, sym.end = int(enclosing.Start.Line)+1, int(enclosing.End.Line)+1
				}
			}
		}
	}
	err = st.Index().Tx(ctx, func(tx *sql.Tx) error {
		// The table names are this literal slice, never input. A table name
		// cannot be a bound parameter, which is why the statement is built
		// this way rather than parameterised.
		for _, table := range []string{"scip_occurrences", "scip_relationships", "scip_symbols"} {
			//nolint:gosec // G202: concatenates a constant from the slice above, never input
			if _, err := tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE repository_id=?`, repoID); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM nodes WHERE repository_id=? AND json_extract(attrs,'$.source')='scip'`, repoID); err != nil {
			return err
		}
		for name, sym := range symbols {
			signature := sym.info.GetSignatureDocumentation().GetText()
			display := displayName(sym.info, name)
			docs, _ := json.Marshal(sym.info.Documentation)
			res, err := tx.ExecContext(ctx, `INSERT INTO nodes(workspace_id,repository_id,kind,name,fqn,file_id,start_line,end_line,signature,content_hash,attrs,index_version) VALUES(?,?,?,?,?,?,?,?,?,?,'{"source":"scip"}',?)`, st.ID().String(), repoID, sym.kind, display, name, sym.src.fileID, sym.start, sym.end, signature, sym.src.hash, version.IndexerVersion)
			if err != nil {
				return err
			}
			sym.nodeID, err = res.LastInsertId()
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO scip_symbols VALUES(?,?,?,?,?,?,?,?,?,?,?)`, st.ID().String(), repoID, name, display, sym.kind, sym.src.doc.RelativePath, sym.start, signature, string(docs), sym.src.hash, sym.nodeID); err != nil {
				return err
			}
			stats.Symbols++
		}
		// Declarations grouped by the file they are in, so the enclosing-owner
		// search below is over one file rather than the whole repository.
		inFile := make(map[*source][]*symbol, len(sources))
		for _, sym := range symbols {
			inFile[sym.src] = append(inFile[sym.src], sym)
		}
		edge := func(src, dst int64, kind string) error {
			_, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO edges(workspace_id,src_id,dst_id,kind,evidence,source,index_version) VALUES(?,?,?,?,'resolved','scip',?)`, st.ID().String(), src, dst, kind, version.IndexerVersion)
			return err
		}
		for _, src := range sources {
			file := src.doc.RelativePath
			stats.Documents++
			for _, occ := range src.doc.Occurrences {
				r, _ := occ.SourceRange()
				name := scoped(file, occ.Symbol)
				if _, err := tx.ExecContext(ctx, `INSERT INTO scip_occurrences VALUES(?,?,?,?,?,?,?,?,?,?)`, st.ID().String(), repoID, name, file, r.Start.Line+1, r.Start.Character, r.End.Line+1, r.End.Character, occ.SymbolRoles, src.hash); err != nil {
					return err
				}
				stats.Occurrences++
				if occ.SymbolRoles&int32(scip.SymbolRole_Definition) != 0 {
					continue
				}
				target := symbols[name]
				if target == nil {
					continue
				}
				ownerID := src.nodeID
				span := int(^uint(0) >> 1)
				// Only this file's symbols can enclose this occurrence, and
				// scanning all of them instead was quadratic in the size of
				// the repository: Django's index holds ~10^5 symbols and
				// ~10^6 occurrences, and the import stopped finishing rather
				// than being slow. The candidates are grouped by file once,
				// above, which makes this a scan over one file's declarations.
				for _, owner := range inFile[src] {
					if owner.nodeID != target.nodeID && owner.start <= int(r.Start.Line)+1 &&
						owner.end >= int(r.End.Line)+1 && owner.end-owner.start < span {
						ownerID = owner.nodeID
						span = owner.end - owner.start
					}
				}
				if err := edge(ownerID, target.nodeID, "references"); err != nil {
					return err
				}
			}
		}
		for name, sym := range symbols {
			for _, rel := range sym.info.Relationships {
				other := scoped(sym.src.doc.RelativePath, rel.Symbol)
				for _, link := range []struct {
					active bool
					kind   string
				}{{rel.IsImplementation, "implements"}, {rel.IsReference, "references"}, {rel.IsTypeDefinition, "uses_type"}, {rel.IsDefinition, "references"}} {
					if !link.active {
						continue
					}
					if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO scip_relationships VALUES(?,?,?,?,?)`, st.ID().String(), repoID, name, other, link.kind); err != nil {
						return err
					}
					stats.Relationships++
					if target := symbols[other]; target != nil {
						if err := edge(sym.nodeID, target.nodeID, link.kind); err != nil {
							return err
						}
					}
				}
			}
		}
		return nil
	})
	return stats, err
}
