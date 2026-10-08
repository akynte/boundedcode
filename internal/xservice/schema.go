package xservice

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Schema sources for SQL contracts: .sql files (migrations and queries
// alike: each statement is classified), Liquibase changelogs (YAML and XML),
// MyBatis mapper XML and Prisma schemas.

// analyzeSQLFiles reads .sql files.
func analyzeSQLFiles(repo, root string, relFiles []string) []Endpoint {
	var out []Endpoint
	for _, rel := range relFiles {
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil || len(b) > 4<<20 {
			continue
		}
		out = append(out, sqlEndpoints(repo, filepath.ToSlash(rel), 1, string(b), Exact, "", "")...)
	}
	return out
}

// Liquibase change types and the attributes naming their tables.
var liquibaseTableAttrs = []string{"tableName", "baseTableName", "oldTableName", "newTableName", "viewName"}

func isLiquibase(doc *yaml.Node) bool {
	n := doc
	if n.Kind == yaml.DocumentNode && len(n.Content) > 0 {
		n = n.Content[0]
	}
	return mapGet(n, "databaseChangeLog") != nil
}

// liquibaseYAML emits the tables a YAML changelog changes.
func liquibaseYAML(repo, rel string, doc *yaml.Node) []Endpoint {
	var out []Endpoint
	seen := map[string]bool{}
	walkYAML(doc, func(key string, val, keyNode *yaml.Node) {
		if val.Kind != yaml.MappingNode {
			return
		}
		schema := ""
		if s := mapGet(val, "schemaName"); s != nil {
			schema = s.Value + "."
		}
		for _, attr := range liquibaseTableAttrs {
			if t := mapGet(val, attr); t != nil && t.Kind == yaml.ScalarNode && t.Value != "" {
				table := strings.ToLower(schema + t.Value)
				k := key + "|" + table + "|" + filepath.ToSlash(rel)
				if !seen[k] {
					seen[k] = true
					out = append(out, Endpoint{Kind: SQLSchema, Repo: repo, File: filepath.ToSlash(rel), Line: keyNode.Line, Table: table,
						Confidence: Exact, Detail: "liquibase " + key})
				}
			}
		}
		if key == "sql" { // an inline SQL change
			if s := mapGet(val, "sql"); s != nil {
				out = append(out, sqlEndpoints(repo, filepath.ToSlash(rel), s.Line, s.Value, Exact, "", "liquibase")...)
			}
		}
	}, nil)
	return out
}

var (
	liquibaseXMLChangeRE = regexp.MustCompile(`<(createTable|addColumn|dropColumn|renameColumn|dropTable|modifyDataType|createIndex|dropIndex|addPrimaryKey|addNotNullConstraint|dropNotNullConstraint|addUniqueConstraint|dropUniqueConstraint|addDefaultValue|dropDefaultValue|addForeignKeyConstraint|dropForeignKeyConstraint|renameTable|createView|dropView|renameView|addAutoIncrement|mergeColumns|insert|update|delete|loadData)\b([^>]*)>`)
	xmlAttrRE            = regexp.MustCompile(`\b(\w+)\s*=\s*"([^"]*)"`)
	mybatisStatementRE   = regexp.MustCompile(`(?s)<(select|insert|update|delete)\b[^>]*>(.*?)</(?:select|insert|update|delete)>`)
	xmlTagRE             = regexp.MustCompile(`(?s)<[^>]*>`)
	xmlDynamicRE         = regexp.MustCompile(`[#$]\{[^}]*\}`)
	liquibaseSQLRE       = regexp.MustCompile(`(?s)<sql\b[^>]*>(.*?)</sql>`)
)

// analyzeXML reads Liquibase XML changelogs and MyBatis mappers; other XML
// is skipped after a cheap check.
func analyzeXML(repo, root string, relFiles []string) []Endpoint {
	var out []Endpoint
	for _, rel := range relFiles {
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil || len(b) > 4<<20 {
			continue
		}
		src := string(b)
		rel = filepath.ToSlash(rel)
		lineAt := func(off int) int { return strings.Count(src[:off], "\n") + 1 }
		switch {
		case strings.Contains(src, "databaseChangeLog"):
			for _, m := range liquibaseXMLChangeRE.FindAllStringSubmatchIndex(src, -1) {
				change := src[m[2]:m[3]]
				attrs := map[string]string{}
				for _, a := range xmlAttrRE.FindAllStringSubmatch(src[m[4]:m[5]], -1) {
					attrs[a[1]] = a[2]
				}
				schema := ""
				if s := attrs["schemaName"]; s != "" {
					schema = s + "."
				}
				kind := SQLSchema
				switch change {
				case "insert", "update", "delete", "loadData":
					kind = SQLAccess
				}
				for _, attr := range liquibaseTableAttrs {
					if t := attrs[attr]; t != "" {
						out = append(out, Endpoint{Kind: kind, Repo: repo, File: rel, Line: lineAt(m[0]), Table: strings.ToLower(schema + t),
							Confidence: Exact, Detail: "liquibase " + change})
					}
				}
			}
			// <sql> changes hold raw SQL.
			for _, m := range liquibaseSQLRE.FindAllStringSubmatchIndex(src, -1) {
				out = append(out, sqlEndpoints(repo, rel, lineAt(m[2]), xmlText(src[m[2]:m[3]]), Exact, "", "liquibase")...)
			}
		case strings.Contains(src, "<mapper"):
			for _, m := range mybatisStatementRE.FindAllStringSubmatchIndex(src, -1) {
				body := xmlDynamicRE.ReplaceAllString(xmlText(src[m[4]:m[5]]), "?")
				conf := Exact
				if strings.Contains(src[m[4]:m[5]], "${") {
					conf = Partial // ${} substitutes text, possibly a table name
				}
				out = append(out, sqlEndpoints(repo, rel, lineAt(m[4]), body, conf, "", "mybatis")...)
			}
		}
	}
	return out
}

// xmlText removes tags (MyBatis <if>, <where>, ...) and decodes the basic
// entities and CDATA.
func xmlText(s string) string {
	s = strings.ReplaceAll(s, "<![CDATA[", "")
	s = strings.ReplaceAll(s, "]]>", "")
	s = xmlTagRE.ReplaceAllStringFunc(s, func(tag string) string { return strings.Repeat("\n", strings.Count(tag, "\n")) + " " })
	return strings.NewReplacer("&lt;", "<", "&gt;", ">", "&amp;", "&", "&quot;", `"`, "&apos;", "'").Replace(s)
}

var (
	prismaModelRE = regexp.MustCompile(`(?m)^\s*model\s+(\w+)\s*\{`)
	prismaMapRE   = regexp.MustCompile(`@@map\(\s*(?:name\s*:\s*)?"([^"]+)"`)
	prismaSchema  = regexp.MustCompile(`@@schema\(\s*"([^"]+)"`)
)

// analyzePrisma reads Prisma schemas: each model is a table (its @@map
// name, else the model name). It also returns the client accessor ->
// table map (prisma.paymentEvent -> table of model PaymentEvent).
func analyzePrisma(repo, root string, relFiles []string) ([]Endpoint, map[string]string) {
	var out []Endpoint
	models := map[string]string{}
	for _, rel := range relFiles {
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil || len(b) > 4<<20 {
			continue
		}
		src := string(b)
		for _, m := range prismaModelRE.FindAllStringSubmatchIndex(src, -1) {
			name := src[m[2]:m[3]]
			body := src[m[1]:]
			if end := strings.IndexByte(body, '}'); end >= 0 {
				body = body[:end]
			}
			table := name
			if mm := prismaMapRE.FindStringSubmatch(body); mm != nil {
				table = mm[1]
			}
			if sm := prismaSchema.FindStringSubmatch(body); sm != nil {
				table = sm[1] + "." + table
			}
			table = strings.ToLower(table)
			models[strings.ToLower(name)] = table
			out = append(out, Endpoint{Kind: SQLSchema, Repo: repo, File: filepath.ToSlash(rel), Line: strings.Count(src[:m[0]], "\n") + 1,
				Symbol: name, Table: table, Confidence: Exact, Detail: "prisma model"})
		}
	}
	return out, models
}

// resolvePrisma turns Prisma client accessors into tables: the model's
// mapped name when the repository has the schema, else the model name.
func resolvePrisma(eps []Endpoint, models map[string]string) {
	for i, e := range eps {
		if acc, ok := strings.CutPrefix(e.Table, prismaPrefix); ok {
			if t, ok := models[acc]; ok {
				eps[i].Table = t
			} else {
				eps[i].Table = acc
				eps[i].Confidence = Partial
			}
		}
	}
}
