package xservice

import (
	"os"
	"path/filepath"
	"strings"
)

// jsAnalyzer extracts endpoints from one JS/TS file.
type jsAnalyzer struct {
	repo, rel string
	toks      []jsTok
	bindings  map[string][]jsTok // const/let/var NAME = <expr tokens>
	axios     map[string]string  // NAME bound to axios.create({baseURL}) -> base path
	out       []Endpoint
	diags     []Diagnostic
	// Evaluation bounds (see budget.go): binding values are computed once
	// per file, a binding being evaluated is a cycle, and each top-level
	// evaluation has a step budget.
	memo       map[string]jsValue
	evaluating map[string]bool
	budget     evalBudget
	overBudget bool // a diagnostic was emitted for this file
}

// jsValue is a memoized binding value.
type jsValue struct {
	s  string
	ok bool
}

func analyzeJS(repo, root string, relFiles []string) ([]Endpoint, []Diagnostic) {
	var out []Endpoint
	var diags []Diagnostic
	for _, rel := range relFiles {
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil || len(b) > 2<<20 {
			continue
		}
		a := &jsAnalyzer{repo: repo, rel: filepath.ToSlash(rel), toks: jsLex(string(b)), bindings: map[string][]jsTok{}, axios: map[string]string{},
			memo: map[string]jsValue{}, evaluating: map[string]bool{}}
		a.collectBindings()
		a.scan()
		out = append(out, a.out...)
		diags = append(diags, a.diags...)
	}
	return out, diags
}

// continuesExpr are operators that, ending a line or starting the next one,
// continue an expression across a newline.
var continuesExpr = map[string]bool{"+": true, "-": true, "*": true, "/": true, "%": true, ".": true, "?.": true,
	"?": true, ":": true, "||": true, "&&": true, "??": true, "=": true, "=>": true, "(": true, "[": true, ",": true,
	"|": true, "&": true, "==": true, "===": true, "!=": true, "!==": true, "<": true, ">": true, "<=": true, ">=": true}

func continues(tk jsTok) bool { return tk.kind == jsPunct && continuesExpr[tk.text] }

// stmtEnd is exprEnd for a binding's initializer: it also stops at a newline
// at nesting depth 0 when neither side of it continues the expression, so
// that in code without semicolons a binding does not swallow the following
// statements (automatic semicolon insertion, approximately).
func stmtEnd(t []jsTok, i int) int {
	start, depth := i, 0
	for ; i < len(t); i++ {
		if depth == 0 && i > start && t[i].line > t[i-1].line && !continues(t[i-1]) && !continues(t[i]) {
			return i
		}
		if t[i].kind != jsPunct {
			continue
		}
		switch t[i].text {
		case "(", "[", "{":
			depth++
		case ")", "]", "}":
			if depth == 0 {
				return i
			}
			depth--
		case ",", ";":
			if depth == 0 {
				return i
			}
		}
	}
	return i
}

// exprEnd returns the index just past the expression starting at i: it stops
// at a top-level ',', ')', ']', '}' or ';' (or a newline-terminated statement
// boundary is approximated by ';').
func exprEnd(t []jsTok, i int) int {
	depth := 0
	for ; i < len(t); i++ {
		if t[i].kind != jsPunct {
			continue
		}
		switch t[i].text {
		case "(", "[", "{":
			depth++
		case ")", "]", "}":
			if depth == 0 {
				return i
			}
			depth--
		case ",", ";":
			if depth == 0 {
				return i
			}
		}
	}
	return i
}

func (a *jsAnalyzer) collectBindings() {
	t := a.toks
	for i := 0; i+3 < len(t); i++ {
		if t[i].kind != jsIdent || (t[i].text != "const" && t[i].text != "let" && t[i].text != "var") {
			continue
		}
		if t[i+1].kind != jsIdent {
			continue
		}
		name := t[i+1].text
		j := i + 2
		if t[j].kind == jsPunct && t[j].text == ":" { // TS type annotation
			for j < len(t) && (t[j].kind != jsPunct || t[j].text != "=") {
				if t[j].kind == jsPunct && (t[j].text == ";" || t[j].text == ",") {
					break
				}
				j++
			}
		}
		if j >= len(t) || t[j].text != "=" {
			continue
		}
		end := stmtEnd(t, j+1)
		expr := t[j+1 : end]
		a.bindings[name] = expr
		// axios.create({ baseURL: ... })
		if len(expr) >= 4 && expr[0].text == "axios" && expr[1].text == "." && expr[2].text == "create" {
			base := ""
			if v, ok := objectValue(expr[3:], "baseURL"); ok {
				s, _ := a.eval(v, 0)
				base = s
			}
			a.axios[name] = base
		}
	}
}

// objectValue finds `key: <expr>` inside an object-literal token range and
// returns the value tokens.
func objectValue(t []jsTok, key string) ([]jsTok, bool) {
	depth := 0
	for i := 0; i < len(t); i++ {
		if t[i].kind == jsPunct {
			switch t[i].text {
			case "{", "(", "[":
				depth++
			case "}", ")", "]":
				depth--
			}
			continue
		}
		if depth == 1 || depth == 2 { // inside the call's (...{ ... })
			if (t[i].kind == jsIdent || t[i].kind == jsString) && t[i].text == key && i+1 < len(t) && t[i+1].text == ":" {
				end := exprEnd(t, i+2)
				return t[i+2 : end], true
			}
		}
	}
	return nil, false
}

// eval resolves an expression's token list to a string; unresolved parts
// become "{}". The bool reports full resolution.
func (a *jsAnalyzer) eval(t []jsTok, depth int) (string, bool) {
	if depth == 0 {
		a.budget = evalBudget{}
	}
	if depth > 10 || len(t) == 0 || !a.budget.step() {
		a.noteBudget(t)
		return "{}", false
	}
	var b strings.Builder
	ok := true
	for i := 0; i < len(t); i++ {
		if b.Len() > maxEvalLen {
			a.budget.exhausted = true // a value this long is not an address
		}
		if a.budget.exhausted {
			a.noteBudget(t)
			s, _ := capLen(b.String())
			return s, false
		}
		tk := t[i]
		switch {
		case tk.kind == jsString:
			b.WriteString(tk.text)
		case tk.kind == jsTemplate:
			for k, ch := range tk.chunks {
				b.WriteString(ch)
				if k < len(tk.subs) {
					v, sok := a.eval(tk.subs[k], depth+1)
					b.WriteString(v)
					ok = ok && sok
				}
			}
		case tk.kind == jsIdent && (tk.text == "process" || tk.text == "import") && i+3 < len(t) && t[i+1].text == "." && t[i+2].text == "env":
			// process.env.X ?? "default": take the fallback if present.
			b.WriteString("{}")
			ok = false
			i += 3
			if i+1 < len(t) && (t[i+1].text == "??" || t[i+1].text == "||") {
				v, _ := a.eval(t[i+2:], depth+1)
				b.Reset()
				b.WriteString(v)
				return b.String(), false
			}
		case tk.kind == jsIdent:
			if bound, found := a.bindings[tk.text]; found && (i+1 >= len(t) || t[i+1].text != "(") {
				v, sok := a.binding(tk.text, bound, depth+1)
				b.WriteString(v)
				ok = ok && sok
			} else {
				b.WriteString("{}")
				ok = false
				// Skip a member/call chain on an unknown identifier.
				for i+2 < len(t) && (t[i+1].text == "." || t[i+1].text == "?.") && t[i+2].kind == jsIdent {
					i += 2
				}
			}
		case tk.kind == jsPunct && tk.text == "+":
		case tk.kind == jsPunct && (tk.text == "(" || tk.text == ")"):
		default:
			b.WriteString("{}")
			ok = false
		}
	}
	s, fits := capLen(b.String())
	return s, ok && fits
}

// binding evaluates a named binding once per file. A binding reached again
// while it is being evaluated (x = f(x), or a = b; b = a) is unresolved.
func (a *jsAnalyzer) binding(name string, bound []jsTok, depth int) (string, bool) {
	if v, ok := a.memo[name]; ok {
		return v.s, v.ok
	}
	if a.evaluating[name] {
		return "{}", false
	}
	a.evaluating[name] = true
	s, ok := a.eval(bound, depth)
	delete(a.evaluating, name)
	// A value cut short by the depth limit or the budget is not memoized:
	// it depends on where it was reached from.
	if !a.budget.exhausted && depth <= 1 {
		a.memo[name] = jsValue{s, ok}
	}
	return s, ok
}

// noteBudget records, once per file, that evaluation was cut short.
func (a *jsAnalyzer) noteBudget(t []jsTok) {
	if !a.budget.exhausted || a.overBudget {
		return
	}
	a.overBudget = true
	line := 0
	if len(t) > 0 {
		line = t[0].line
	}
	a.diags = append(a.diags, Diagnostic{File: a.rel, Line: line, Message: "constant evaluation budget exceeded; values in this file may be unresolved"})
}

func (a *jsAnalyzer) emit(line int, e Endpoint) {
	e.Repo, e.File, e.Line = a.repo, a.rel, line
	a.out = append(a.out, e)
}

var jsHTTPVerbs = map[string]string{"get": "GET", "post": "POST", "put": "PUT", "patch": "PATCH", "delete": "DELETE", "head": "HEAD", "options": "OPTIONS", "all": ""}

func (a *jsAnalyzer) scan() {
	t := a.toks
	controller := "" // NestJS @Controller prefix (file scope)
	for i := 0; i < len(t); i++ {
		tk := t[i]
		if tk.kind == jsString || tk.kind == jsTemplate {
			i = a.scanSQL(i) - 1
			continue
		}
		if tk.kind != jsIdent {
			continue
		}
		next := func(k int) string {
			if i+k < len(t) {
				return t[i+k].text
			}
			return ""
		}
		switch {
		// NestJS decorators.
		case tk.text == "@Controller" && next(1) == "(":
			end := exprEnd(t, i+2)
			if v, ok := a.eval(span(t, i+2, end), 0); ok {
				controller = v
			} else if end == i+2 {
				controller = ""
			}
		case strings.HasPrefix(tk.text, "@") && next(1) == "(" && jsHTTPVerbs[strings.ToLower(tk.text[1:])] != "" && tk.text[1] >= 'A' && tk.text[1] <= 'Z':
			end := exprEnd(t, i+2)
			sub := ""
			if end > i+2 {
				v, ok := a.eval(span(t, i+2, end), 0)
				if !ok {
					continue
				}
				sub = v
			}
			p := "/" + strings.Trim(strings.Trim(controller, "/")+"/"+strings.Trim(sub, "/"), "/")
			a.emit(tk.line, Endpoint{Kind: HTTPRoute, Method: jsHTTPVerbs[strings.ToLower(tk.text[1:])], Path: NormalizePath(p), Confidence: Resolved, Detail: "nestjs " + tk.text})
		// fetch(url, { method })
		case tk.text == "fetch" && next(1) == "(" && (i == 0 || t[i-1].text != "."):
			end := exprEnd(t, i+2)
			method := "GET"
			if end < len(t) && t[end].text == "," {
				oend := exprEnd(t, end+1)
				if v, ok := objectValue(span(t, end, oend+1), "method"); ok {
					if s, ok := a.eval(v, 0); ok {
						method = strings.ToUpper(s)
					}
				}
			}
			a.emitCall(tk.line, method, span(t, i+2, end), "fetch")
		// axios.verb(url) / axios({ url, method }) / instance.verb(path)
		case next(1) == "." && jsHTTPVerbs[next(2)] != "" && next(3) == "(" && (tk.text == "axios" || a.axios[tk.text] != "" || isAxiosBinding(a, tk.text)):
			end := exprEnd(t, i+4)
			base := a.axios[tk.text]
			urlToks := span(t, i+4, end)
			if base != "" {
				s, ok := a.eval(urlToks, 0)
				a.emitResolvedCall(tk.line, jsHTTPVerbs[next(2)], joinPath(NormalizePath(base), s), ok, "axios instance")
			} else {
				a.emitCall(tk.line, jsHTTPVerbs[next(2)], urlToks, "axios")
			}
		case tk.text == "axios" && next(1) == "(":
			end := exprEnd(t, i+2)
			obj := span(t, i+1, end+1)
			if u, ok := objectValue(obj, "url"); ok {
				method := "GET"
				if m, ok := objectValue(obj, "method"); ok {
					if s, ok := a.eval(m, 0); ok {
						method = strings.ToUpper(s)
					}
				}
				a.emitCall(tk.line, method, u, "axios")
			}
		// Express/Fastify/Koa-router/Hono: X.verb('/path', handler)
		case next(1) == "." && jsHTTPVerbs[next(2)] != "" || (next(1) == "." && next(2) == "all"):
			if next(3) != "(" || i+4 >= len(t) || (t[i+4].kind != jsString && t[i+4].kind != jsTemplate && t[i+4].kind != jsIdent) {
				continue
			}
			end := exprEnd(t, i+4)
			if end >= len(t) || t[end].text != "," { // routes always have a handler
				continue
			}
			p, ok := a.eval(span(t, i+4, end), 0)
			if !ok || !strings.HasPrefix(p, "/") {
				continue
			}
			a.emit(tk.line, Endpoint{Kind: HTTPRoute, Method: jsHTTPVerbs[next(2)], Path: NormalizePath(p), Confidence: Resolved, Detail: tk.text + "." + next(2)})
		// kafkajs: producer.send({ topic }), consumer.subscribe({ topic | topics })
		case next(1) == "." && (next(2) == "send" || next(2) == "sendBatch" || next(2) == "subscribe") && next(3) == "(":
			end := exprEnd(t, i+4)
			obj := span(t, i+3, end+1)
			k := TopicProduce
			if next(2) == "subscribe" {
				k = TopicConsume
			}
			if v, ok := objectValue(obj, "topic"); ok {
				a.emitTopic(tk.line, k, v, "kafkajs "+next(2))
			}
			if v, ok := objectValue(obj, "topics"); ok && k == TopicConsume {
				for _, el := range splitArray(v) {
					a.emitTopic(tk.line, k, el, "kafkajs subscribe")
				}
			}
		// process.env.NAME / process.env["NAME"] / import.meta.env.NAME
		case (tk.text == "process" && next(1) == "." && next(2) == "env") || (tk.text == "import" && next(1) == "." && next(2) == "meta" && next(3) == "." && next(4) == "env"):
			j := i + 3
			if tk.text == "import" {
				j = i + 5
			}
			if j+1 < len(t) && t[j].text == "." && t[j+1].kind == jsIdent {
				a.emit(tk.line, Endpoint{Kind: EnvRead, Env: t[j+1].text, Confidence: Exact, Detail: tk.text + ".env"})
			} else if j+2 < len(t) && t[j].text == "[" && t[j+1].kind == jsString {
				a.emit(tk.line, Endpoint{Kind: EnvRead, Env: t[j+1].text, Confidence: Exact, Detail: tk.text + ".env"})
			}
		}
	}
}

func isAxiosBinding(a *jsAnalyzer, name string) bool {
	_, ok := a.axios[name]
	return ok
}

func splitArray(t []jsTok) [][]jsTok {
	if len(t) < 2 || t[0].text != "[" {
		return [][]jsTok{t}
	}
	var out [][]jsTok
	i := 1
	for i < len(t) {
		end := exprEnd(t, i)
		if end > i {
			out = append(out, t[i:end])
		}
		if end >= len(t) || t[end].text == "]" {
			break
		}
		i = end + 1
	}
	return out
}

func (a *jsAnalyzer) emitCall(line int, method string, urlToks []jsTok, detail string) {
	s, ok := a.eval(urlToks, 0)
	a.emitResolvedCall(line, method, s, ok, detail)
}

func (a *jsAnalyzer) emitResolvedCall(line int, method, s string, ok bool, detail string) {
	p := NormalizePath(s)
	if p == "" {
		a.diags = append(a.diags, Diagnostic{File: a.rel, Line: line, Message: "http call with unresolved URL"})
		return
	}
	conf := Resolved
	if !ok {
		conf = Partial
	}
	a.emit(line, Endpoint{Kind: HTTPCall, Method: method, Path: p, Confidence: conf, Detail: detail})
}

func (a *jsAnalyzer) emitTopic(line int, k Kind, v []jsTok, detail string) {
	s, ok := a.eval(v, 0)
	if !ok || s == "" {
		a.diags = append(a.diags, Diagnostic{File: a.rel, Line: line, Message: "unresolved topic"})
		return
	}
	a.emit(line, Endpoint{Kind: k, Topic: s, Confidence: Resolved, Detail: detail})
}

// span returns t[lo:hi] clamped to valid bounds (malformed input must not panic).
func span(t []jsTok, lo, hi int) []jsTok {
	hi = min(hi, len(t))
	if lo < 0 || lo >= hi {
		return nil
	}
	return t[lo:hi]
}

// scanSQL analyzes the string at t[i], joined with strings concatenated to
// it by +, as SQL when it is. Template substitutions become "{}", so a
// table named by a substitution is not guessed. It returns the index after
// the strings.
func (a *jsAnalyzer) scanSQL(i int) int {
	t := a.toks
	var b strings.Builder
	line, conf := t[i].line, Exact
	j := i
	for j < len(t) && (t[j].kind == jsString || t[j].kind == jsTemplate) {
		if t[j].kind == jsTemplate {
			b.WriteString(strings.Join(t[j].chunks, "{}"))
			if len(t[j].subs) > 0 {
				conf = Resolved
			}
		} else {
			b.WriteString(t[j].text)
		}
		j++
		if j+1 < len(t) && t[j].kind == jsPunct && t[j].text == "+" && (t[j+1].kind == jsString || t[j+1].kind == jsTemplate) {
			j++
			conf = worse(conf, Resolved)
			continue
		}
		break
	}
	if v := b.String(); looksLikeSQL(v) && !sourceTestPath(a.rel) {
		a.out = append(a.out, sqlEndpoints(a.repo, a.rel, line, v, conf, "", "query")...)
	}
	return j
}
