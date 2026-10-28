package verify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"time"
	"unicode/utf8"
)

// pyMutator finds mutation points with Python's own tokenizer (strings,
// comments and f-strings are tokens, never mutated) and keeps a mutant
// only when Python's parser accepts it. It reads the file and runs no
// project code (python3 -I: no site packages, no current directory).
const pyMutator = `
import ast, io, json, sys, tokenize
src = open(sys.argv[1], encoding="utf-8").read()
lines = set(json.loads(sys.argv[2]))
ops = {"<": ">=", "<=": ">", ">": "<=", ">=": "<", "==": "!=", "!=": "==", "+": "-", "-": "+"}
shift = {"<": "<=", "<=": "<", ">": ">=", ">=": ">"}
names = {"and": "or", "or": "and", "True": "False", "False": "True"}
after_value = {")", "]", "}"}
src_lines = src.splitlines(keepends=True)
toks = list(tokenize.generate_tokens(io.StringIO(src).readline))
out = []
def emit(t, repl, kind):
    row = t.start[0] - 1
    line = src_lines[row]
    mutated = "".join(src_lines[:row]) + line[:t.start[1]] + repl + line[t.end[1]:] + "".join(src_lines[row + 1:])
    try:
        ast.parse(mutated)
    except SyntaxError:
        return
    out.append({"line": t.start[0], "col": t.start[1], "end": t.end[1], "orig": t.string, "repl": repl, "op": kind})
for i, t in enumerate(toks):
    if t.start[0] not in lines or t.start[0] != t.end[0]:
        continue
    prev = toks[i - 1] if i else None
    repl = kind = None
    if t.type == tokenize.OP and t.string in ops:
        if t.string in "+-":
            # Binary only: after a name, number, string or closing bracket.
            if prev is None or not (prev.type in (tokenize.NAME, tokenize.NUMBER, tokenize.STRING) or prev.string in after_value):
                continue
            if prev.type == tokenize.NAME and prev.string in ("return", "in", "not", "and", "or", "if", "else", "yield", "lambda", "is", "await"):
                continue
            kind = "arithmetic"
        else:
            kind = "relational"
        repl = ops[t.string]
    elif t.type == tokenize.NAME and t.string in names:
        repl, kind = names[t.string], ("logical" if t.string in ("and", "or") else "constant")
    elif t.type == tokenize.NUMBER and t.string.isdigit() and prev is not None and prev.type == tokenize.OP and prev.string in ("<", "<=", ">", ">=", "==", "!="):
        repl, kind = str(int(t.string) + 1), "boundary"
    elif t.type == tokenize.NUMBER and t.string.isdigit() and prev is not None and prev.type == tokenize.NAME and prev.string == "return":
        repl, kind = str(int(t.string) + 1), "return"
    if repl is None:
        continue
    cands = [(repl, kind)]
    if kind == "relational" and t.string in shift:
        cands.append((shift[t.string], "boundary"))
    for repl, kind in cands:
        emit(t, repl, kind)

print(json.dumps(out))
`

type pyPoint struct {
	Line int    `json:"line"`
	Col  int    `json:"col"` // characters, not bytes
	End  int    `json:"end"`
	Orig string `json:"orig"`
	Repl string `json:"repl"`
	Op   string `json:"op"`
}

func pythonMutants(ctx context.Context, rel string, src []byte, lines map[int]bool) ([]Mutant, error) {
	py, err := exec.LookPath("python3")
	if err != nil {
		return nil, errors.New("python3 is not installed")
	}
	dir, err := os.MkdirTemp("", "bc-pymut-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	f := filepath.Join(dir, "m.py")
	if err := os.WriteFile(f, src, 0o600); err != nil {
		return nil, err
	}
	var ls []int
	for l := range lines {
		ls = append(ls, l)
	}
	lj, _ := json.Marshal(ls)
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	c := exec.CommandContext(cctx, py, "-I", "-c", pyMutator, f, string(lj))
	c.Dir = dir
	out, err := c.Output()
	if err != nil {
		return nil, nil //nolint:nilerr // the file does not tokenize: no mutants
	}
	var pts []pyPoint
	if err := json.Unmarshal(out, &pts); err != nil {
		return nil, err
	}
	// Character columns to byte offsets.
	lineStart := []int{0}
	for i, b := range src {
		if b == '\n' {
			lineStart = append(lineStart, i+1)
		}
	}
	var ms []Mutant
	for _, p := range pts {
		if p.Line-1 >= len(lineStart) {
			continue
		}
		ls := lineStart[p.Line-1]
		lineEnd := len(src)
		if i := bytes.IndexByte(src[ls:], '\n'); i >= 0 {
			lineEnd = ls + i
		}
		start, end := charToByte(src[ls:lineEnd], p.Col), charToByte(src[ls:lineEnd], p.End)
		if start < 0 || end < 0 || string(src[ls+start:ls+end]) != p.Orig {
			continue
		}
		ms = append(ms, Mutant{File: rel, Line: p.Line, Start: ls + start, End: ls + end, Orig: p.Orig, Repl: p.Repl, Operator: p.Op})
	}
	return ms, nil
}

func charToByte(line []byte, col int) int {
	off := 0
	for i := 0; i < col; i++ {
		if off >= len(line) {
			if i == col {
				return off
			}
			return -1
		}
		_, n := utf8.DecodeRune(line[off:])
		off += n
	}
	return off
}
