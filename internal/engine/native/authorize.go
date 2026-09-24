package native

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/akynte/boundedcode/internal/engine"
	"github.com/akynte/boundedcode/internal/ledger"
	"github.com/akynte/boundedcode/internal/llm"
	"github.com/akynte/boundedcode/internal/worktree"
)

// authorize validates the actual advertised schema, not just parseable JSON.
// The native schemas use flat objects with string/integer properties and enums.
func (e *Engine) authorize(req engine.Request, call llm.ToolCall) error {
	var def *llm.ToolDef
	for i := range e.tools {
		if e.tools[i].Name == call.Name {
			def = &e.tools[i]
			break
		}
	}
	if def == nil {
		return fmt.Errorf("tool %q is not advertised for this attempt", call.Name)
	}
	var schema struct {
		Required   []string `json:"required"`
		Properties map[string]struct {
			Type string   `json:"type"`
			Enum []string `json:"enum"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(def.Schema, &schema); err != nil {
		return fmt.Errorf("invalid supervisor tool schema: %w", err)
	}
	var args map[string]any
	if err := json.Unmarshal(call.Arguments, &args); err != nil || args == nil {
		return fmt.Errorf("arguments must be a valid JSON object matching the tool schema")
	}
	for _, key := range schema.Required {
		if _, ok := args[key]; !ok {
			return fmt.Errorf("missing required argument %q", key)
		}
	}
	for key, value := range args {
		property, ok := schema.Properties[key]
		if !ok {
			return fmt.Errorf("unknown argument %q", key)
		}
		switch property.Type {
		case "string":
			text, ok := value.(string)
			if !ok {
				return fmt.Errorf("%s must be a string", key)
			}
			if len(property.Enum) > 0 {
				found := false
				for _, choice := range property.Enum {
					found = found || choice == text
				}
				if !found {
					return fmt.Errorf("%s is not an allowed value", key)
				}
			}
		case "integer":
			n, ok := value.(float64)
			if !ok || math.Trunc(n) != n || n < 0 || n > math.MaxInt32 {
				return fmt.Errorf("%s must be a nonnegative 32-bit integer", key)
			}
		default:
			return fmt.Errorf("unsupported supervisor schema type %q", property.Type)
		}
	}
	switch call.Name {
	case ToolReadFile, ToolWriteFile, ToolEditFile:
		return req.Access.Check(req.Worktree, str(args, "path"), call.Name != ToolReadFile)
	case ToolListFiles:
		dir := str(args, "dir")
		if dir == "" {
			dir = "."
		}
		return req.Access.Check(req.Worktree, dir, false)
	}
	return nil
}

// expectedNativeEdit derives the file-level facts recovery needs before an
// edit crosses the side-effect boundary. The engine still performs its normal
// validation and formatting; this is a journal payload, not a second policy.
// An invalid edit remains inspectable with the path and before-hash, while a
// valid one gets the exact after-hash the recovery classifier compares.
func expectedNativeEdit(req engine.Request, call llm.ToolCall, args map[string]any) (ledger.EditIntent, bool) {
	if call.Name != ToolEditFile && call.Name != ToolWriteFile {
		return ledger.EditIntent{}, false
	}
	rel := str(args, "path")
	if rel == "" {
		return ledger.EditIntent{}, false
	}
	ei := ledger.EditIntent{Path: rel}
	full := filepath.Join(req.Worktree, filepath.FromSlash(rel))
	if before, err := ledger.HashFile(full); err == nil {
		ei.BeforeHash = before
	}
	var after []byte
	switch call.Name {
	case ToolWriteFile:
		if _, err := os.Stat(full); err == nil {
			return ei, true
		}
		after, _ = gofmtOnWrite(rel, []byte(str(args, "content")))
	case ToolEditFile:
		body, err := worktree.ReadWithin(req.Worktree, rel)
		if err != nil {
			return ei, true
		}
		oldText, newText := str(args, "old"), str(args, "new")
		if oldText != "" && strings.Count(string(body), oldText) == 1 {
			after, _ = gofmtOnWrite(rel, []byte(strings.Replace(string(body), oldText, newText, 1)))
		}
	}
	if after != nil {
		sum := sha256.Sum256(after)
		ei.AfterHash = hex.EncodeToString(sum[:])
	}
	return ei, true
}

// exec records decisions before a tool can have side effects. Journal failure
// stops the attempt; it must not turn into an unrecorded edit.
func (e *Engine) exec(ctx context.Context, req engine.Request, call llm.ToolCall) (Result, error) {
	denial := e.authorize(req, call)
	before := ""
	if req.Journal != nil {
		var err error
		before, err = ledger.ContentManifestContext(ctx, req.Worktree)
		if err != nil {
			return Result{}, err
		}
	}
	var h *ledger.Handle
	if req.Journal != nil {
		decision, reason := "allow", ""
		if denial != nil {
			decision, reason = "deny", denial.Error()
		}
		var args map[string]any
		_ = json.Unmarshal(call.Arguments, &args)
		kind := ledger.KindDecision
		var intent any = map[string]any{
			"phase": "EDIT", "tool": call.Name, "call_id": call.ID,
			"arguments": string(call.Arguments), "decision": decision, "reason": reason,
			"session": e.ensureFence().Token(), "step": req.Transcript.Steps,
		}
		if editIntent, ok := expectedNativeEdit(req, call, args); ok {
			kind = ledger.KindEdit
			editIntent.Tool = call.Name
			editIntent.CallID = call.ID
			editIntent.Arguments = string(call.Arguments)
			editIntent.Session = e.ensureFence().Token()
			editIntent.Step = req.Transcript.Steps
			editIntent.Decision = decision
			editIntent.Reason = reason
			intent = editIntent
		}
		var err error
		h, err = req.Journal.Begin(ctx, req.TaskID, kind, intent, before)
		if err != nil {
			return Result{}, err
		}
	}
	var result Result
	if denial != nil {
		result = failed("firewall: %v", denial)
		result.Invalid = true
	} else {
		result = e.execute(ctx, req, call)
	}
	if h != nil {
		after, err := ledger.ContentManifestContext(ctx, req.Worktree)
		if err != nil {
			return result, err
		}
		if err := h.Complete(ctx, result, after, ""); err != nil {
			return result, err
		}
	}
	return result, nil
}
