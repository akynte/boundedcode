package xservice

import (
	"strings"
)

// SQL contracts. A table is a contract between the code that defines it
// (DDL, migrations, schema files) and the code that queries it; when two
// services share a database, a migration in one repository can break
// queries in another. SQL text is tokenized (comments, quoted identifiers,
// string and dollar-quoted literals are understood) and each statement is
// classified: CREATE/ALTER/DROP/RENAME TABLE, CREATE INDEX and CREATE VIEW
// define a table (SQLSchema); SELECT/INSERT/UPDATE/DELETE/MERGE/COPY/
// TRUNCATE use one (SQLAccess). Names from CTEs, subqueries and function
// arguments (EXTRACT(x FROM y)) are not tables.

// sqlTok is one SQL token.
type sqlTok struct {
	kind sqlTokKind
	text string // words upper-cased in up; identifiers keep text
	up   string
	line int // 0-based line offset within the text
}

type sqlTokKind int

const (
	sqlWord sqlTokKind = iota
	sqlQuotedIdent
	sqlString
	sqlNumber
	sqlPunct
	sqlParam // $1, ?, :name, @p
)

// sqlLex tokenizes SQL. It is tolerant: malformed input never fails.
func sqlLex(s string) []sqlTok {
	var out []sqlTok
	line := 0
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == '\n':
			line++
			i++
		case c == ' ' || c == '\t' || c == '\r' || c == '\f':
			i++
		case c == '-' && i+1 < len(s) && s[i+1] == '-':
			for i < len(s) && s[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < len(s) && s[i+1] == '*':
			i += 2
			for i < len(s) && (s[i] != '*' || i+1 >= len(s) || s[i+1] != '/') {
				if s[i] == '\n' {
					line++
				}
				i++
			}
			i += 2
		case c == '\'':
			start := line
			j := i + 1
			for j < len(s) {
				if s[j] == '\'' {
					if j+1 < len(s) && s[j+1] == '\'' {
						j += 2
						continue
					}
					break
				}
				if s[j] == '\\' && j+1 < len(s) {
					j++
				}
				if s[j] == '\n' {
					line++
				}
				j++
			}
			out = append(out, sqlTok{kind: sqlString, line: start})
			i = j + 1
		case c == '"' || c == '`' || c == '[':
			end := byte('"')
			switch c {
			case '`':
				end = '`'
			case '[':
				end = ']'
			}
			j := i + 1
			for j < len(s) && s[j] != end && s[j] != '\n' {
				j++
			}
			if j >= len(s) || s[j] != end {
				i++ // not an identifier ([ in an array type, a stray quote)
				out = append(out, sqlTok{kind: sqlPunct, text: string(c), up: string(c), line: line})
				continue
			}
			id := s[i+1 : j]
			out = append(out, sqlTok{kind: sqlQuotedIdent, text: id, up: strings.ToUpper(id), line: line})
			i = j + 1
		case c == '$' && i+1 < len(s) && (s[i+1] == '$' || isSQLIdentStart(s[i+1])):
			// $tag$ ... $tag$ (PostgreSQL bodies), or a $name parameter.
			j := i + 1
			for j < len(s) && isSQLIdentPart(s[j]) {
				j++
			}
			if j < len(s) && s[j] == '$' {
				tag := s[i : j+1]
				k := strings.Index(s[j+1:], tag)
				body := s[j+1:]
				if k >= 0 {
					body = s[j+1 : j+1+k]
				}
				out = append(out, sqlTok{kind: sqlString, line: line})
				line += strings.Count(body, "\n")
				i = j + 1 + len(body) + len(tag)
				continue
			}
			out = append(out, sqlTok{kind: sqlParam, text: s[i:j], line: line})
			i = j
		case c == '$' || c == '?' || ((c == ':' || c == '@') && i+1 < len(s) && isSQLIdentStart(s[i+1])):
			j := i + 1
			for j < len(s) && (isSQLIdentPart(s[j])) {
				j++
			}
			out = append(out, sqlTok{kind: sqlParam, text: s[i:j], line: line})
			i = j
		case c >= '0' && c <= '9':
			j := i
			for j < len(s) && (s[j] >= '0' && s[j] <= '9' || s[j] == '.') {
				j++
			}
			out = append(out, sqlTok{kind: sqlNumber, text: s[i:j], line: line})
			i = j
		case isSQLIdentStart(c):
			j := i
			for j < len(s) && isSQLIdentPart(s[j]) {
				j++
			}
			w := s[i:j]
			out = append(out, sqlTok{kind: sqlWord, text: w, up: strings.ToUpper(w), line: line})
			i = j
		default:
			out = append(out, sqlTok{kind: sqlPunct, text: string(c), up: string(c), line: line})
			i++
		}
	}
	return out
}

func isSQLIdentStart(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80
}

func isSQLIdentPart(c byte) bool { return isSQLIdentStart(c) || c >= '0' && c <= '9' || c == '$' }

// sqlRef is a table a statement defines or uses.
type sqlRef struct {
	table  string
	kind   Kind   // SQLSchema or SQLAccess
	detail string // "create table", "select", ...
	line   int    // 0-based within the text
}

// sqlStatementStarts are the first keywords of the statements recognised.
var sqlStatementStarts = map[string]bool{"SELECT": true, "INSERT": true, "UPDATE": true, "DELETE": true, "WITH": true,
	"CREATE": true, "ALTER": true, "DROP": true, "MERGE": true, "REPLACE": true, "TRUNCATE": true, "UPSERT": true,
	"COPY": true, "RENAME": true}

// sqlNotTable are words that can follow FROM/JOIN/INTO/UPDATE without being
// a table.
var sqlNotTable = map[string]bool{"SELECT": true, "LATERAL": true, "ONLY": true, "SET": true, "VALUES": true, "DUAL": true,
	"UNNEST": true, "WHERE": true, "AS": true, "ON": true, "USING": true, "TABLE": true, "DEFAULT": true, "STDIN": true,
	"STDOUT": true, "IGNORE": true, "IF": true, "EXISTS": true, "NOT": true, "OR": true, "AND": true, "NULL": true}

// sqlClauseKeywords end a FROM list.
var sqlClauseKeywords = map[string]bool{"WHERE": true, "GROUP": true, "ORDER": true, "HAVING": true, "LIMIT": true,
	"OFFSET": true, "UNION": true, "EXCEPT": true, "INTERSECT": true, "RETURNING": true, "ON": true, "USING": true,
	"JOIN": true, "INNER": true, "LEFT": true, "RIGHT": true, "FULL": true, "CROSS": true, "NATURAL": true, "WINDOW": true,
	"FOR": true, "SET": true, "VALUES": true, "FETCH": true, "INTO": true, "OUTER": true, "STRAIGHT_JOIN": true}

// sqlGroupingWords open a parenthesis that is not a function call.
var sqlGroupingWords = map[string]bool{"IN": true, "EXISTS": true, "AS": true, "ON": true, "FROM": true, "JOIN": true,
	"VALUES": true, "ANY": true, "ALL": true, "SOME": true, "LATERAL": true, "USING": true, "WITH": true, "SELECT": true,
	"WHERE": true, "AND": true, "OR": true, "NOT": true, "INTO": true, "TABLE": true, "UNION": true, "THEN": true,
	"ELSE": true, "WHEN": true, "RETURNING": true, "BY": true, "HAVING": true, "MATERIALIZED": true, "SET": true}

// analyzeSQL returns the tables defined or used by the statements in s.
func analyzeSQL(s string) []sqlRef {
	toks := sqlLex(s)
	var out []sqlRef
	start := 0
	for i := 0; i <= len(toks); i++ {
		if i == len(toks) || toks[i].kind == sqlPunct && toks[i].text == ";" {
			if i > start {
				out = append(out, sqlStatement(toks[start:i])...)
			}
			start = i + 1
		}
	}
	return out
}

// sqlName reads a possibly qualified name at t[i]; it returns the
// lower-cased name and the index after it ("" if there is none).
func sqlName(t []sqlTok, i int) (string, int) {
	var parts []string
	for i < len(t) {
		tk := t[i]
		if tk.kind != sqlWord && tk.kind != sqlQuotedIdent {
			break
		}
		parts = append(parts, strings.ToLower(tk.text))
		if i+2 < len(t) && t[i+1].kind == sqlPunct && t[i+1].text == "." {
			i += 2
			continue
		}
		i++
		break
	}
	if len(parts) == 0 {
		return "", i
	}
	return strings.Join(parts, "."), i
}

func at(t []sqlTok, i int) string {
	if i >= 0 && i < len(t) {
		return t[i].up
	}
	return ""
}

func skipWords(t []sqlTok, i int, words ...string) int {
	for i < len(t) {
		matched := false
		for _, w := range words {
			if t[i].kind == sqlWord && t[i].up == w {
				matched = true
				break
			}
		}
		if !matched {
			return i
		}
		i++
	}
	return i
}

// sqlStatement classifies one statement.
func sqlStatement(t []sqlTok) []sqlRef {
	if len(t) == 0 || t[0].kind != sqlWord {
		return nil
	}
	var out []sqlRef
	add := func(name string, k Kind, detail string, line int) {
		if name == "" || sqlSystemTable(name) {
			return
		}
		out = append(out, sqlRef{table: name, kind: k, detail: detail, line: line})
	}
	switch t[0].up {
	case "CREATE":
		i := skipWords(t, 1, "OR", "REPLACE", "GLOBAL", "LOCAL", "UNLOGGED", "EXTERNAL", "VIRTUAL", "FOREIGN", "IF", "NOT", "EXISTS")
		temp := false
		for at(t, i) == "TEMP" || at(t, i) == "TEMPORARY" {
			temp = true
			i++
		}
		switch at(t, i) {
		case "TABLE":
			i = skipWords(t, i+1, "IF", "NOT", "EXISTS")
			if name, _ := sqlName(t, i); !temp {
				add(name, SQLSchema, "create table", t[0].line)
			}
			return out
		case "MATERIALIZED", "VIEW":
			i = skipWords(t, i, "MATERIALIZED", "VIEW", "IF", "NOT", "EXISTS")
			if name, _ := sqlName(t, i); !temp {
				add(name, SQLSchema, "create view", t[0].line)
			}
			return out
		case "UNIQUE", "INDEX":
			for j := i; j < len(t); j++ {
				if t[j].kind == sqlWord && t[j].up == "ON" {
					name, _ := sqlName(t, skipWords(t, j+1, "ONLY"))
					add(name, SQLSchema, "create index", t[0].line)
					break
				}
			}
			return out
		}
		return nil
	case "ALTER":
		if at(t, 1) != "TABLE" {
			return nil
		}
		name, _ := sqlName(t, skipWords(t, 2, "IF", "EXISTS", "ONLY"))
		add(name, SQLSchema, "alter table", t[0].line)
		return out
	case "DROP":
		materializedView := at(t, 1) == "MATERIALIZED" && at(t, 2) == "VIEW"
		if at(t, 1) != "TABLE" && at(t, 1) != "VIEW" && !materializedView {
			return nil
		}
		i := skipWords(t, 1, "MATERIALIZED", "TABLE", "VIEW", "IF", "EXISTS")
		for i < len(t) {
			name, j := sqlName(t, i)
			add(name, SQLSchema, "drop table", t[0].line)
			if j >= len(t) || t[j].text != "," {
				break
			}
			i = j + 1
		}
		return out
	case "RENAME":
		if at(t, 1) == "TABLE" {
			name, _ := sqlName(t, 2)
			add(name, SQLSchema, "rename table", t[0].line)
		}
		return out
	case "TRUNCATE":
		name, _ := sqlName(t, skipWords(t, 1, "TABLE", "ONLY"))
		add(name, SQLAccess, "truncate", t[0].line)
		return out
	case "COPY":
		name, _ := sqlName(t, 1)
		add(name, SQLAccess, "copy", t[0].line)
		return out
	}
	if !sqlStatementStarts[t[0].up] {
		return nil
	}
	// DML, possibly with CTEs: walk every FROM/JOIN/INTO/UPDATE/USING.
	ctes := map[string]bool{}
	if t[0].up == "WITH" {
		i := skipWords(t, 1, "RECURSIVE")
		for i < len(t) {
			name, j := sqlName(t, i)
			if name == "" {
				break
			}
			ctes[name] = true
			// name [(cols)] AS [NOT] [MATERIALIZED] ( ... ) [, next]
			if at(t, j) == "(" {
				j = matchParen(t, j)
			}
			j = skipWords(t, j, "AS", "NOT", "MATERIALIZED")
			if at(t, j) != "(" {
				break
			}
			j = matchParen(t, j)
			if j >= len(t) || t[j].text != "," {
				break
			}
			i = j + 1
		}
	}
	var parens []bool // true: a function call's parenthesis
	inCall := func() bool {
		for _, p := range parens {
			if p {
				return true
			}
		}
		return false
	}
	for i := 0; i < len(t); i++ {
		tk := t[i]
		if tk.kind == sqlPunct {
			switch tk.text {
			case "(":
				prev := at(t, i-1)
				call := i > 0 && (t[i-1].kind == sqlWord || t[i-1].kind == sqlQuotedIdent) && !sqlGroupingWords[prev]
				parens = append(parens, call)
			case ")":
				if len(parens) > 0 {
					parens = parens[:len(parens)-1]
				}
			}
			continue
		}
		if tk.kind != sqlWord || inCall() {
			continue
		}
		switch tk.up {
		case "FROM":
			if at(t, i-1) == "DISTINCT" { // IS [NOT] DISTINCT FROM
				continue
			}
			detail := "select"
			if at(t, i-1) == "DELETE" {
				detail = "delete"
			}
			// FROM a [alias], b [alias] ... until a clause keyword.
			j := skipWords(t, i+1, "ONLY")
			for j < len(t) {
				if at(t, j) == "(" || sqlNotTable[at(t, j)] && t[j].kind == sqlWord {
					break
				}
				name, k := sqlName(t, j)
				if name == "" || at(t, k) == "(" { // a function: generate_series(...)
					break
				}
				if !ctes[name] {
					add(name, SQLAccess, detail, tk.line)
				}
				// skip alias
				k = skipWords(t, k, "AS")
				if k < len(t) && (t[k].kind == sqlWord && !sqlClauseKeywords[t[k].up] || t[k].kind == sqlQuotedIdent) {
					k++
				}
				if k >= len(t) || t[k].text != "," {
					break
				}
				j = k + 1
			}
		case "JOIN", "USING":
			if tk.up == "USING" && at(t, i+1) == "(" {
				continue // JOIN ... USING (col)
			}
			j := skipWords(t, i+1, "ONLY", "LATERAL")
			if name, k := sqlName(t, j); name != "" && at(t, k) != "(" && !sqlNotTable[strings.ToUpper(name)] && !ctes[name] {
				add(name, SQLAccess, "select", tk.line)
			}
		case "INTO":
			if i == 0 || (at(t, i-1) != "INSERT" && at(t, i-1) != "MERGE" && at(t, i-1) != "REPLACE" && at(t, i-1) != "IGNORE" && at(t, i-1) != "UPSERT") {
				continue // SELECT ... INTO var
			}
			if name, _ := sqlName(t, i+1); name != "" && !ctes[name] {
				add(name, SQLAccess, strings.ToLower(at(t, skipBack(t, i-1, "IGNORE"))), tk.line)
			}
		case "UPDATE":
			if at(t, i-1) == "DO" || at(t, i-1) == "KEY" || at(t, i-1) == "FOR" || at(t, i-1) == "ON" {
				continue // ON CONFLICT DO UPDATE, ON DUPLICATE KEY UPDATE, FOR UPDATE
			}
			j := skipWords(t, i+1, "ONLY", "LOW_PRIORITY", "IGNORE")
			name, k := sqlName(t, j)
			k = skipWords(t, k, "AS")
			if at(t, k) != "SET" && k+1 < len(t) && t[k].kind == sqlWord {
				k++ // alias
			}
			if name != "" && at(t, k) == "SET" && !ctes[name] {
				add(name, SQLAccess, "update", tk.line)
			}
		}
	}
	return out
}

func skipBack(t []sqlTok, i int, word string) int {
	if at(t, i) == word {
		return i - 1
	}
	return i
}

// matchParen returns the index after the parenthesis group starting at i.
func matchParen(t []sqlTok, i int) int {
	depth := 0
	for ; i < len(t); i++ {
		if t[i].kind != sqlPunct {
			continue
		}
		switch t[i].text {
		case "(":
			depth++
		case ")":
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return i
}

func sqlSystemTable(name string) bool {
	for _, p := range []string{"information_schema.", "pg_catalog.", "sys.", "mysql.", "performance_schema."} {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	switch name {
	case "dual", "sqlite_master", "sqlite_schema", "sqlite_sequence", "pg_class", "pg_tables", "pg_indexes", "schema_migrations":
		return true
	}
	return strings.HasPrefix(name, "pg_") || strings.Contains(name, "{}")
}

// looksLikeSQL reports whether a string literal in code is a SQL statement
// rather than prose ("delete from cart failed"): it must start with a
// statement keyword (any case only for multi-line text; otherwise upper
// case, the near-universal convention) and parse into a recognised
// statement shape.
func looksLikeSQL(s string) bool {
	trimmed := strings.TrimLeft(s, " \t\r\n(")
	for strings.HasPrefix(trimmed, "--") || strings.HasPrefix(trimmed, "/*") {
		if strings.HasPrefix(trimmed, "--") {
			i := strings.IndexByte(trimmed, '\n')
			if i < 0 {
				return false
			}
			trimmed = strings.TrimLeft(trimmed[i+1:], " \t\r\n(")
		} else {
			i := strings.Index(trimmed, "*/")
			if i < 0 {
				return false
			}
			trimmed = strings.TrimLeft(trimmed[i+2:], " \t\r\n(")
		}
	}
	end := 0
	for end < len(trimmed) && isSQLIdentPart(trimmed[end]) {
		end++
	}
	first := trimmed[:end]
	if !sqlStatementStarts[strings.ToUpper(first)] {
		return false
	}
	if first != strings.ToUpper(first) && !strings.Contains(strings.TrimSpace(s), "\n") {
		return false
	}
	u := strings.ToUpper(trimmed)
	switch strings.ToUpper(first) {
	case "SELECT", "WITH":
		return strings.Contains(u, " FROM ") || strings.Contains(u, "\nFROM ") || strings.Contains(u, "\tFROM ")
	case "INSERT", "REPLACE", "UPSERT", "MERGE":
		return strings.Contains(u, "INTO ")
	case "UPDATE":
		return strings.Contains(u, " SET ") || strings.Contains(u, "\nSET ")
	case "DELETE":
		return strings.Contains(u, "FROM ")
	case "CREATE", "ALTER", "DROP", "RENAME":
		return strings.Contains(u, "TABLE") || strings.Contains(u, "INDEX") || strings.Contains(u, "VIEW")
	}
	return true
}

// sqlEndpoints turns the tables of SQL text into endpoints. line is the
// text's first line in the file; partial marks text built at run time.
func sqlEndpoints(repo, rel string, line int, text string, conf Confidence, symbol, via string) []Endpoint {
	var out []Endpoint
	seen := map[string]bool{}
	for _, r := range analyzeSQL(text) {
		k := string(r.kind) + "|" + r.table + "|" + r.detail
		if seen[k] {
			continue
		}
		seen[k] = true
		detail := r.detail
		if via != "" {
			detail += " (" + via + ")"
		}
		out = append(out, Endpoint{Kind: r.kind, Repo: repo, File: rel, Line: line + r.line, Symbol: symbol, Table: r.table,
			Confidence: conf, Detail: detail})
	}
	return out
}

// tablesMatch compares table names; an unqualified name matches the same
// name in any schema ("payments" and "billing.payments").
func tablesMatch(a, b string) bool {
	if a == b {
		return true
	}
	la, lb := a[strings.LastIndexByte(a, '.')+1:], b[strings.LastIndexByte(b, '.')+1:]
	return la == lb && (!strings.Contains(a, ".") || !strings.Contains(b, "."))
}
