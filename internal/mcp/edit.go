package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/akynte/boundedcode/internal/artifacts"
	"github.com/akynte/boundedcode/internal/firewall"
	"github.com/akynte/boundedcode/internal/ledger"
	"github.com/akynte/boundedcode/internal/policy"
	"github.com/akynte/boundedcode/internal/store"
	"github.com/akynte/boundedcode/internal/supervisor"
	"github.com/akynte/boundedcode/internal/task"
	"github.com/akynte/boundedcode/internal/trust"
	"github.com/akynte/boundedcode/internal/worktree"
)

// The editing tools exist so the session's file access goes through the
// firewall instead of around it (architecture review §4, §9).
//
// An OpenCode session has its own read and edit tools, and they answer to
// OpenCode's permission system — which the review is explicit about: its
// enforcement has documented bypasses, so it is a convenience layer and not the
// boundary. The sandbox keeps the session inside the worktree, which is the
// containment that actually holds. What the sandbox cannot express is the rest
// of §9's path policy: a `.env` committed inside the repository is inside the
// worktree, and so is a generated file, and so is every path the plan did not
// declare.
//
// So the confined session runs with those built-in tools denied and these in
// their place. Every call goes through the same firewall.Access the native
// editor uses, which is the point: one path policy, one implementation, and no
// second route that has to be kept in step with it.

// readIn is the proxied read.
type readIn struct {
	TaskID    string `json:"task_id" jsonschema:"the supervised task ID; reads are always candidate-bound"`
	Path      string `json:"path" jsonschema:"repository-relative path to read"`
	StartLine int    `json:"start_line,omitempty" jsonschema:"first line, 1-based; omit for the start"`
	EndLine   int    `json:"end_line,omitempty" jsonschema:"last line, inclusive; omit for the end"`
	Root      string `json:"root,omitempty" jsonschema:"a subdirectory of the open repository; defaults to its root"`
}

type readOut struct {
	Evidence  string `json:"evidence,omitempty"`
	Path      string `json:"path"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	Content   string `json:"content"`
	Truncated bool   `json:"truncated,omitempty"`
}

// maxReadBytes bounds one read. A model that asks for a 40,000-line generated
// file gets the beginning of it and a note, rather than a phase whose context
// budget is gone.
const maxReadBytes = 256 << 10

func (s *Server) readFile(ctx context.Context, req *mcp.CallToolRequest, in readIn) (*mcp.CallToolResult, readOut, error) {
	if strings.TrimSpace(in.TaskID) == "" {
		return fail("task_id is required: every supervised read must name the authoritative task worktree"), readOut{}, nil
	}
	sess, err := s.resolve(ctx, in.Root)
	if err != nil {
		return fail("%v", err), readOut{}, nil
	}
	defer sess.Close() //nolint:contextcheck // cleanup must not take the request context: a cancelled call would then skip closing the databases.

	root := sess.Workspace.Root
	if in.TaskID != "" {
		_, wt, release, err := supervisor.AuthorizeEditorOperation(ctx, sess.Store, sess.Workspace.Root, in.TaskID, openCodeSessionID(req), supervisor.EditorRead)
		if err != nil {
			return fail("%v", err), readOut{}, nil
		}
		defer release()
		root = wt.Path
	}
	access := firewall.Access{Protected: protectedSet(root)}
	if err := access.Check(root, in.Path, false); err != nil {
		return fail("%v", err), readOut{}, nil
	}
	body, err := worktree.ReadWithin(root, in.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return fail("%s does not exist", in.Path), readOut{}, nil
		}
		return fail("reading %s: %v", in.Path, err), readOut{}, nil
	}
	fileHashSum := sha256.Sum256(body)
	fileHash := hex.EncodeToString(fileHashSum[:])
	out := readOut{Path: in.Path, StartLine: 1}
	if len(body) > maxReadBytes {
		body, out.Truncated = body[:maxReadBytes], true
	}
	lines := strings.Split(string(body), "\n")
	start, end := 1, len(lines)
	if in.StartLine > 0 {
		start = in.StartLine
	}
	if in.EndLine > 0 && in.EndLine < end {
		end = in.EndLine
	}
	if start > len(lines) {
		return fail("%s has %d lines; start_line %d is past the end", in.Path, len(lines), start), readOut{}, nil
	}
	out.StartLine, out.EndLine = start, end
	out.Content = strings.Join(lines[start-1:end], "\n")
	if in.TaskID != "" {
		// A read with a task id also records an immutable artifact and durable
		// tool observation. It is therefore a mutation of the task journal, not
		// a harmless read; the binding and lease were checked before the read.
		if _, err := task.NewStore(sess.Store).Get(ctx, in.TaskID); err != nil {
			return fail("%v", err), readOut{}, nil
		}
		phase, err := supervisor.CurrentPhase(ctx, sess.Store, in.TaskID)
		if err != nil {
			return fail("recording read phase: %v", err), readOut{}, nil
		}
		hash, err := artifacts.New(sess.Store).Put([]byte(out.Content))
		if err != nil {
			return fail("recording read evidence: %v", err), readOut{}, nil
		}
		out.Evidence = hash
		_, err = supervisor.RecordMemory(ctx, sess.Store, in.TaskID, supervisor.MemoryRecord{
			Type: "tool_observation", Source: "tool", SourceTool: "bc_read",
			Text:     fmt.Sprintf("Read %s lines %d-%d", in.Path, start, end),
			Evidence: hash, Path: in.Path, FileHash: fileHash,
			StartLine: start, EndLine: end, Repository: string(sess.Workspace.ID()), Phase: phase,
		}, false)
		if err != nil {
			return fail("recording read observation: %v", err), readOut{}, nil
		}
	}

	// Fenced with its provenance, like every other route that puts repository
	// content in front of a model. A file that says "ignore your instructions"
	// is a finding, not an instruction, and the marker is what says so.
	fence, err := trust.NewFence()
	if err != nil {
		return fail("%v", err), readOut{}, nil
	}
	origin := fmt.Sprintf("source=file path=%s lines=%d-%d evidence=%s", in.Path, start, end, out.Evidence)
	return text(fence.Preamble() + "\n" + fence.Wrap(origin, out.Content)), out, nil
}

// editIn is the proxied edit: exact string replacement, not a whole-file write.
type editIn struct {
	TaskID string `json:"task_id" jsonschema:"the id bc_task_start returned; its declared scope is what bounds this write"`
	Path   string `json:"path" jsonschema:"repository-relative path to change"`
	Old    string `json:"old" jsonschema:"exact text to replace, including indentation. It must appear exactly once"`
	New    string `json:"new" jsonschema:"replacement text"`
	Root   string `json:"root,omitempty" jsonschema:"a subdirectory of the open repository; defaults to its root"`
}

type editOut struct {
	Path         string `json:"path"`
	Replacements int    `json:"replacements"`
	Candidate    string `json:"candidate,omitempty"`
}

func beginEdit(ctx context.Context, store *store.Store, taskID, root, path string, after []byte) (*ledger.Handle, error) {
	candidate, err := ledger.ContentManifestContext(ctx, root)
	if err != nil {
		return nil, err
	}
	phase, err := supervisor.CurrentPhase(ctx, store, taskID)
	if err != nil {
		return nil, err
	}
	intent := ledger.EditIntent{Path: path, AfterHash: hashBytes(after), Phase: phase}
	if before, hashErr := ledger.HashFile(filepath.Join(root, filepath.FromSlash(path))); hashErr == nil {
		intent.BeforeHash = before
	}
	return ledger.New(store).Begin(ctx, taskID, ledger.KindEdit, intent, candidate)
}

func hashBytes(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func completeEdit(ctx context.Context, h *ledger.Handle, root, path string) (string, error) {
	candidate, err := ledger.ContentManifestContext(ctx, root)
	if err != nil {
		return "", err
	}
	if err := h.Complete(ctx, map[string]any{
		"path": path, "candidate": candidate,
	}, candidate, ""); err != nil {
		return "", err
	}
	return candidate, nil
}

func (s *Server) editFile(ctx context.Context, req *mcp.CallToolRequest, in editIn) (*mcp.CallToolResult, editOut, error) {
	if strings.TrimSpace(in.TaskID) == "" {
		return fail("task_id is required: a write outside a supervised task has nothing to bound it. Call bc_task_start first"), editOut{}, nil
	}
	if in.Old == in.New {
		return fail("old and new are identical; nothing to do"), editOut{}, nil
	}
	sess, err := s.resolve(ctx, in.Root)
	if err != nil {
		return fail("%v", err), editOut{}, nil
	}
	defer sess.Close() //nolint:contextcheck // cleanup must not take the request context: a cancelled call would then skip closing the databases.

	t, wt, release, err := supervisor.AuthorizeEditorOperation(ctx, sess.Store, sess.Workspace.Root, in.TaskID, openCodeSessionID(req), supervisor.EditorEdit)
	if err != nil {
		return fail("%v", err), editOut{}, nil
	}
	defer release()
	if t.State == task.StatePending {
		if err := task.NewStore(sess.Store).SetState(ctx, in.TaskID, task.StateRunning); err != nil {
			return fail("recording task start: %v", err), editOut{}, nil
		}
	}
	root := wt.Path
	access := firewall.Access{WriteScope: t.Budget.Scope, Protected: protectedSet(sess.Workspace.Root)}
	if err := access.Check(root, in.Path, true); err != nil {
		// The refusal names the rule, so the next attempt is a re-plan rather
		// than the same edit with a different spelling.
		return fail("%v", err), editOut{}, nil
	}

	body, err := worktree.ReadWithin(root, in.Path)
	if err != nil {
		if os.IsNotExist(err) {
			// A new file is a write too, and the scope check above already
			// decided whether this one is allowed.
			if in.Old != "" {
				return fail("%s does not exist; pass an empty old to create it", in.Path), editOut{}, nil
			}
			// A new file is a write too, and the scope check above already
			// decided whether that one is allowed. Journal the intent before
			// crossing the side-effect boundary, not after the write succeeds.
			h, err := beginEdit(ctx, sess.Store, in.TaskID, root, in.Path, []byte(in.New))
			if err != nil {
				return fail("recording edit intent: %v", err), editOut{}, nil
			}
			if err := worktree.WriteWithin(root, in.Path, []byte(in.New)); err != nil {
				if journalErr := h.Interrupted(ctx, err); journalErr != nil {
					return fail("creating %s failed (%v) and the edit journal could not record the uncertainty: %v", in.Path, err, journalErr), editOut{}, nil
				}
				return fail("creating %s: %v", in.Path, err), editOut{}, nil
			}
			candidate, err := completeEdit(ctx, h, root, in.Path)
			if err != nil {
				return fail("%s was written but its edit outcome could not be recorded; reconcile the task: %v", in.Path, err), editOut{}, nil
			}
			return text(fmt.Sprintf("Created %s.", in.Path)), editOut{Path: in.Path, Replacements: 1, Candidate: candidate}, nil
		}
		return fail("reading %s: %v", in.Path, err), editOut{}, nil
	}

	// Exactly once, or not at all. A replacement that matched twice would
	// change a line the model never read, and one that matched zero times
	// means it is working from a version of the file that no longer exists —
	// both are worth a refusal the model can act on.
	switch count := strings.Count(string(body), in.Old); count {
	case 1:
	case 0:
		return fail("that exact text is not in %s. Read it again: it may have changed since you last saw it", in.Path), editOut{}, nil
	default:
		return fail("that text appears %d times in %s. Include enough surrounding context to name one of them", count, in.Path), editOut{}, nil
	}
	updated := strings.Replace(string(body), in.Old, in.New, 1)
	h, err := beginEdit(ctx, sess.Store, in.TaskID, root, in.Path, []byte(updated))
	if err != nil {
		return fail("recording edit intent: %v", err), editOut{}, nil
	}
	if err := worktree.WriteWithin(root, in.Path, []byte(updated)); err != nil {
		if journalErr := h.Interrupted(ctx, err); journalErr != nil {
			return fail("writing %s failed (%v) and the edit journal could not record the uncertainty: %v", in.Path, err, journalErr), editOut{}, nil
		}
		return fail("writing %s: %v", in.Path, err), editOut{}, nil
	}
	candidate, err := completeEdit(ctx, h, root, in.Path)
	if err != nil {
		return fail("%s was written but its edit outcome could not be recorded; reconcile the task: %v", in.Path, err), editOut{}, nil
	}
	return text(fmt.Sprintf("Replaced one occurrence in %s. Call bc_verify when the change is complete. Candidate=%s", in.Path, candidate)),
		editOut{Path: in.Path, Replacements: 1, Candidate: candidate}, nil
}

// protectedSet loads the repository's own policy rules, so a proxied write
// answers to the same operator configuration a supervised task does.
func protectedSet(root string) policy.Set {
	set, err := policy.Load(root + "/policies")
	if err != nil {
		return policy.Set{}
	}
	return set
}
