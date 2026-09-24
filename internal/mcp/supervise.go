package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/akynte/boundedcode/internal/artifacts"
	"github.com/akynte/boundedcode/internal/engine"
	"github.com/akynte/boundedcode/internal/firewall"
	"github.com/akynte/boundedcode/internal/ledger"
	"github.com/akynte/boundedcode/internal/policy"
	"github.com/akynte/boundedcode/internal/recipe"
	"github.com/akynte/boundedcode/internal/supervisor"
	"github.com/akynte/boundedcode/internal/task"
	"github.com/akynte/boundedcode/internal/worktree"
)

// registerSupervision adds the tools that put an editor's work under the same
// engineering controls a CLI task gets.
//
// The division of labour is forced by the protocol rather than chosen. An MCP
// server cannot ask a user a question: OpenCode declares only the `roots`
// capability, not `elicitation`, so there is no channel for one. The agent
// talking to the user is the only component that can pause and ask — so the
// agent edits, and this supervises.
//
// What that buys is the whole point: the work is journalled before it happens,
// judged afterwards by the completion contract rather than by the agent's
// account of itself, and every result is tied to the exact content hash it
// describes. An agent that says "done" and an agent that is done become
// distinguishable, which is the property the CLI has always had and an editor
// session never did.
func (s *Server) registerSupervision(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: "bc_task_start",
		Description: "Open a supervised task before changing code. Returns the paths this " +
			"repository protects and the checks that will judge the work. Call this first " +
			"when asked to implement, fix, refactor or change anything; then edit normally.",
	}, s.taskStart)
	mcp.AddTool(srv, &mcp.Tool{
		Name: "bc_task_resume",
		Description: "Bind this OpenCode session to an unfinished supervised task by task_id. " +
			"Use after starting a new OpenCode session when more than one task is active.",
	}, s.taskResume)
	mcp.AddTool(srv, &mcp.Tool{
		Name: "bc_task_history",
		Description: "Retrieve user decisions from the durable task ledger by task_id, " +
			"oldest first. Use offset and limit to page through decisions omitted from the active context card.",
	}, s.taskHistory)
	mcp.AddTool(srv, &mcp.Tool{Name: "bc_task_memory", Description: "Page typed durable task claims and recover immutable evidence by hash. Older claims remain available outside active context."}, s.taskMemory)
	mcp.AddTool(srv, &mcp.Tool{Name: "bc_task_memory_add", Description: "Record a model hypothesis, contradiction, pending action, or failure. The editor cannot assert a confirmed fact or supervisor decision."}, s.taskMemoryAdd)
	mcp.AddTool(srv, &mcp.Tool{Name: "bc_task_fact", Description: "Confirm an exact quote in a permitted repository file and store immutable source evidence. Only this deterministic check can record a repository fact."}, s.taskFact)

	mcp.AddTool(srv, &mcp.Tool{
		Name: "bc_verify",
		Description: "Run this repository's verification recipes in a sandbox against the " +
			"current working tree and apply the completion contract. Returns each check and " +
			"whether the work is accepted. Pass the task_id from bc_task_start so the result " +
			"is recorded against the task. Call after editing, and again after fixing what it " +
			"reports. This, not your own judgement, decides whether a task is done.",
	}, s.verify)

	mcp.AddTool(srv, &mcp.Tool{
		Name: "bc_task_answer",
		Description: "Record a question you had to ask the user and the answer they gave. " +
			"Call this whenever the user resolves an ambiguity you could not infer from the " +
			"codebase — a business rule, an architectural choice, a limit. The answer becomes " +
			"part of this project's record instead of being lost with the conversation.",
	}, s.taskAnswer)

	mcp.AddTool(srv, &mcp.Tool{
		Name: "bc_task_finish",
		Description: "Close a supervised task and produce its final review: what was asked, " +
			"what the user decided, which files changed, what was verified, and the verdict. " +
			"Call after bc_verify reports ACCEPTED. Show the review to the user.",
	}, s.taskFinish)

	// The proxied file tools. A confined session runs with OpenCode's own read
	// and edit denied and these in their place, so one path policy applies to
	// every write rather than two that have to be kept in step.
	mcp.AddTool(srv, &mcp.Tool{
		Name: "bc_read",
		Description: "Read a file through the supervisor's path policy. Secret paths are refused " +
			"rather than returned, and the content comes back marked as repository data.",
	}, s.readFile)
	mcp.AddTool(srv, &mcp.Tool{
		Name: "bc_edit",
		Description: "Change a file through the supervisor's path policy: exact string replacement, " +
			"bounded by the write scope the task declared. Pass an empty old to create a new file. " +
			"A write outside the declared scope is refused and needs a new task, which is what keeps " +
			"an injected instruction from reaching a file the work never mentioned.",
	}, s.editFile)
}

type factIn struct {
	TaskID string `json:"task_id"`
	Path string `json:"path" jsonschema:"repository-relative source path"`
	Quote string `json:"quote" jsonschema:"exact source text to confirm, at most 300 characters"`
}

func (s *Server) taskFact(ctx context.Context, _ *mcp.CallToolRequest, in factIn) (*mcp.CallToolResult, any, error) {
	if in.TaskID=="" || in.Quote=="" || len(in.Quote)>300 {return fail("task_id and a quote of 1..300 bytes are required"),nil,nil}
	sess,err:=s.resolve(ctx,"")
	if err!=nil{return fail("%v",err),nil,nil}
	defer sess.Close()
	if err:=(firewall.Access{Protected:protectedSet(sess.Workspace.Root)}).Check(sess.Workspace.Root,in.Path,false);err!=nil{return fail("%v",err),nil,nil}
	body,err:=worktree.ReadWithin(sess.Workspace.Root,in.Path)
	if err!=nil{return fail("%v",err),nil,nil}
	if len(body)>maxReadBytes {return fail("file exceeds 256 KiB; confirm a smaller source file"),nil,nil}
	if !strings.Contains(string(body),in.Quote) {return fail("exact quote is absent from %s",in.Path),nil,nil}
	hash,err:=artifacts.New(sess.Store).Put(body)
	if err!=nil{return fail("%v",err),nil,nil}
	sum:=sha256.Sum256(body)
	fileHash:=hex.EncodeToString(sum[:])
	id,err:=supervisor.RecordMemory(ctx,sess.Store,in.TaskID,supervisor.MemoryRecord{
		Type:"repository_fact",Text:fmt.Sprintf("At observation time, %s contains exact quote %q",in.Path,in.Quote),
		Evidence:hash,Path:in.Path,FileHash:fileHash,
	},false)
	if err!=nil{return fail("%v",err),nil,nil}
	return text(fmt.Sprintf("Confirmed repository fact #%d; file sha256=%s; evidence=%s",id,fileHash,hash)),map[string]any{"id":id,"file_hash":fileHash,"evidence":hash},nil
}

type memoryIn struct {
	TaskID   string `json:"task_id"`
	Before   int64  `json:"before,omitempty"`
	Limit    int    `json:"limit,omitempty"`
	Evidence string `json:"evidence,omitempty"`
	IncludeObjective bool `json:"include_objective,omitempty"`
	Path     string `json:"path,omitempty"`
}

func (s *Server) taskMemory(ctx context.Context, _ *mcp.CallToolRequest, in memoryIn) (*mcp.CallToolResult, any, error) {
	sess, err := s.resolve(ctx, in.Path)
	if err != nil {
		return fail("%v", err), nil, nil
	}
	defer sess.Close()
	taskInfo, err := task.NewStore(sess.Store).Get(ctx, in.TaskID)
	if err != nil {
		return fail("%v", err), nil, nil
	}
	if in.Evidence != "" {
		var found int
		err := sess.Store.Ledger().SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM operations WHERE task_id=? AND (evidence_id=? OR json_extract(intent,'$.original_prompt_hash')=?)`, in.TaskID, in.Evidence, in.Evidence).Scan(&found)
		if err != nil || found == 0 {
			return fail("evidence is not owned by this task"), nil, nil
		}
		body, err := artifacts.New(sess.Store).Get(in.Evidence)
		if err != nil {
			return fail("%v", err), nil, nil
		}
		return text(string(body)), map[string]any{"evidence": in.Evidence, "content": string(body)}, nil
	}
	if in.Limit == 0 {
		in.Limit = 20
	}
	rows, err := supervisor.MemoryPage(ctx, sess.Store, in.TaskID, in.Before, in.Limit)
	if err != nil {
		return fail("%v", err), nil, nil
	}
	var b strings.Builder
	objective := ""
	if in.IncludeObjective { objective=taskInfo.Title; fmt.Fprintf(&b,"Original objective: %s\n",objective) }
	for _, r := range rows {
		fmt.Fprintf(&b, "#%d [%s] %s evidence=%s candidate=%s supersedes=%d\n", r.ID, r.Type, r.Text, r.Evidence, r.Candidate, r.Supersedes)
	}
	if rows == nil {
		rows = []supervisor.MemoryRecord{}
	}
	return text(b.String()), map[string]any{"records": rows,"objective":objective}, nil
}

type memoryAddIn struct {
	TaskID     string `json:"task_id"`
	Type       string `json:"type" jsonschema:"model_hypothesis, contradicted_hypothesis, tool_observation, open_failure, resolved_failure, or pending_action"`
	Text       string `json:"text"`
	Evidence   string `json:"evidence,omitempty" jsonschema:"immutable artifact hash returned by bc_read"`
	Candidate  string `json:"candidate,omitempty"`
	Supersedes int64  `json:"supersedes,omitempty"`
	Path       string `json:"path,omitempty"`
}

func (s *Server) taskMemoryAdd(ctx context.Context, _ *mcp.CallToolRequest, in memoryAddIn) (*mcp.CallToolResult, any, error) {
	sess, err := s.resolve(ctx, "")
	if err != nil {
		return fail("%v", err), nil, nil
	}
	defer sess.Close()
	id, err := supervisor.RecordMemory(ctx, sess.Store, in.TaskID, supervisor.MemoryRecord{Type: in.Type, Text: in.Text, Evidence: in.Evidence, Candidate: in.Candidate, Supersedes: in.Supersedes, Path: in.Path}, true)
	if err != nil {
		return fail("%v", err), nil, nil
	}
	return text(fmt.Sprintf("Recorded typed task memory #%d (%s).", id, in.Type)), map[string]any{"id": id}, nil
}

// ----------------------------------------------------------- bc_task_answer

type historyIn struct {
	TaskID string `json:"task_id" jsonschema:"the supervised task ID"`
	Offset int    `json:"offset,omitempty" jsonschema:"zero-based decision offset"`
	Limit  int    `json:"limit,omitempty" jsonschema:"number of decisions, 1 to 20; defaults to 10"`
	Path   string `json:"path,omitempty" jsonschema:"a subdirectory of the open repository; defaults to its root"`
}

func (s *Server) taskHistory(ctx context.Context, _ *mcp.CallToolRequest, in historyIn) (*mcp.CallToolResult, any, error) {
	if in.TaskID == "" || in.Offset < 0 || in.Limit < 0 || in.Limit > 20 {
		return fail("task_id, nonnegative offset and limit at most 20 are required"), nil, nil
	}
	if in.Limit == 0 {
		in.Limit = 10
	}
	sess, err := s.resolve(ctx, in.Path)
	if err != nil {
		return fail("%v", err), nil, nil
	}
	defer sess.Close() //nolint:contextcheck // cleanup must complete after a cancelled request.
	if _, err := task.NewStore(sess.Store).Get(ctx, in.TaskID); err != nil {
		return fail("finding task: %v", err), nil, nil
	}
	decisions, total, err := supervisor.DecisionPage(ctx, sess.Store, in.TaskID, in.Offset, in.Limit)
	if err != nil {
		return fail("reading task decisions: %v", err), nil, nil
	}
	if in.Offset > total {
		in.Offset = total
	}
	var b strings.Builder
	fmt.Fprintf(&b, "User decisions %d-%d of %d for task %s:\n", in.Offset, in.Offset+len(decisions), total, in.TaskID)
	for i, d := range decisions {
		fmt.Fprintf(&b, "%d. %s → %s\n", in.Offset+i, d.Question, d.Answer)
	}
	return text(b.String()), nil, nil
}

type answerIn struct {
	TaskID   string `json:"task_id" jsonschema:"the id bc_task_start returned"`
	Question string `json:"question" jsonschema:"what you asked the user"`
	Answer   string `json:"answer" jsonschema:"what they said"`
	Path     string `json:"path,omitempty" jsonschema:"a subdirectory of the open repository; defaults to its root"`
}

// taskAnswer is the division the protocol forces, made useful.
//
// BoundedCode cannot ask the user anything — it has no channel, and the
// agent holding the conversation does. But an answer about this project is a
// decision, and a decision that lives only in a chat transcript is gone by the
// next session. The agent owns the asking; this owns the remembering.
func (s *Server) taskAnswer(ctx context.Context, _ *mcp.CallToolRequest, in answerIn) (*mcp.CallToolResult, any, error) {
	sess, err := s.resolve(ctx, in.Path)
	if err != nil {
		return fail("%v", err), nil, nil
	}
	defer sess.Close() //nolint:contextcheck // cleanup must not take the request context: a cancelled call would then skip closing the databases.

	// The answer is durable and is read back in the final review, so it goes
	// through the same credential check as committed content.
	if err := firewall.CheckContentSecrets("this answer", in.Question+"\n"+in.Answer); err != nil {
		return fail("%v", err), nil, nil
	}
	if err := supervisor.RecordAnswer(ctx, sess.Store, in.TaskID, in.Question, in.Answer); err != nil {
		return fail("recording the decision: %v", err), nil, nil
	}
	return text("Recorded. It will appear in the task's final review and in its journal."), nil, nil
}

// ----------------------------------------------------------- bc_task_finish

type finishIn struct {
	TaskID string `json:"task_id" jsonschema:"the id bc_task_start returned"`
	Path   string `json:"path,omitempty" jsonschema:"a subdirectory of the open repository; defaults to its root"`
}

// taskFinish produces the review artifact.
//
// It is not an approval. The agent has already edited the working tree, so
// there is nothing left to withhold and a gate that blocked here would block
// nothing. What this adds is the part git cannot reconstruct: the objective,
// the decisions the user made along the way, and what the contract concluded.
//
// The verdict comes from the verification actually on record. A task finished
// without one is reported UNVERIFIED rather than fine, because an agent's own
// account of its work is exactly what the contract exists not to trust.
func (s *Server) taskFinish(ctx context.Context, _ *mcp.CallToolRequest, in finishIn) (*mcp.CallToolResult, *supervisor.Review, error) {
	sess, err := s.resolve(ctx, in.Path)
	if err != nil {
		return fail("%v", err), nil, nil
	}
	defer sess.Close() //nolint:contextcheck // cleanup must not take the request context: a cancelled call would then skip closing the databases.

	rev, err := supervisor.FinishTask(ctx, sess.Store, sess.Workspace.Root, in.TaskID)
	if err != nil {
		return fail("finishing the task: %v", err), nil, nil
	}
	return text(rev.Format()), &rev, nil
}

// ------------------------------------------------------------ bc_task_start

type startIn struct {
	Objective    string   `json:"objective" jsonschema:"the user's original objective, concise but faithful"`
	Requirements []string `json:"requirements,omitempty" jsonschema:"explicit user requirements and acceptance criteria that must survive compaction"`
	Constraints  []string `json:"constraints,omitempty" jsonschema:"explicit scope, security and performance constraints from the user"`
	// WriteScope is §9.3's plan-scoped allowlist, declared before the work
	// rather than discovered from the diff afterwards. An injected instruction
	// cannot widen it: adding a path means opening another task, which is a
	// decision a person can see.
	WriteScope []string `json:"write_scope,omitempty" jsonschema:"the repository-relative files you intend to change, including new ones. bc_edit refuses anything outside this. Omit only if you will not use bc_edit"`
	Path       string   `json:"path,omitempty" jsonschema:"a subdirectory of the open repository; defaults to its root"`
}

type startOut struct {
	TaskID        string   `json:"task_id"`
	ProtectedPath []string `json:"protected_paths,omitempty"`
	Checks        []string `json:"checks"`
}

// taskStart opens the journal entry before the work, not after it.
//
// §7.1's order is the reason this is a separate call rather than something
// bc_verify infers: intent is recorded before the side effect, so an
// interrupted session leaves a state that can be reconciled rather than
// guessed at.
func (s *Server) taskStart(ctx context.Context, req *mcp.CallToolRequest, in startIn) (*mcp.CallToolResult, startOut, error) {
	if strings.TrimSpace(in.Objective) == "" {
		return fail("objective is required"), startOut{}, nil
	}
	if err := validateTaskDetails(in.Requirements, in.Constraints); err != nil {
		return fail("%v", err), startOut{}, nil
	}
	sess, err := s.resolve(ctx, in.Path)
	if err != nil {
		return fail("%v", err), startOut{}, nil
	}
	defer sess.Close() //nolint:contextcheck // cleanup must not take the request context: a cancelled call would then skip closing the databases.

	t := task.Task{
		ID:           task.NewID("oc"),
		Title:        strings.TrimSpace(in.Objective),
		Kind:         "supervised",
		Verification: recipe.Standard,
		Budget:       task.Budget{MaxAttempts: 1, MaxWallTime: 30 * time.Minute, Scope: in.WriteScope},
	}
	if err := task.NewStore(sess.Store).Create(ctx, t); err != nil {
		return fail("opening the task: %v", err), startOut{}, nil
	}
	promptHash, err := supervisor.OriginalOpenCodePrompt(ctx, sess.Store, openCodeSessionID(req))
	if err != nil {
		return fail("finding original user prompt: %v", err), startOut{}, nil
	}
	if err := supervisor.RecordEvent(ctx, sess.Store, t.ID, ledger.KindSessionStart,
		map[string]any{"objective": t.Title, "executor": "opencode", "session_id": openCodeSessionID(req),
			"requirements": in.Requirements, "constraints": in.Constraints, "original_prompt_hash": promptHash}); err != nil {
		return fail("recording the OpenCode session: %v", err), startOut{}, nil
	}

	out := startOut{TaskID: t.ID}
	for _, k := range recipe.Required(recipe.Standard) {
		out.Checks = append(out.Checks, string(k))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Task %s opened for: %s\n\n", t.ID, t.Title)

	// The protected paths are the part the agent most needs before it edits.
	// Reporting them afterwards, as a rejection, wastes the work.
	if set, err := policy.Load(sess.Workspace.Root + "/policies"); err == nil && len(set.Policies) > 0 {
		out.ProtectedPath = set.Paths()
		b.WriteString("This repository protects these paths — do not change them:\n")
		for _, p := range out.ProtectedPath {
			fmt.Fprintf(&b, "  %s\n", p)
		}
		b.WriteString("\n")
	}
	if len(t.Budget.Scope) > 0 {
		b.WriteString("Declared write scope — bc_edit refuses anything outside it:\n")
		for _, p := range t.Budget.Scope {
			fmt.Fprintf(&b, "  %s\n", p)
		}
		b.WriteString("\n")
	}
	b.WriteString("Edit normally. When you believe the work is complete, call bc_verify — " +
		"it runs the checks in a sandbox and decides acceptance from the evidence, so " +
		"there is no need to assert that it works.\n")
	return text(b.String()), out, nil
}

func validateTaskDetails(groups ...[]string) error {
	total := 0
	for _, group := range groups {
		if len(group) > 20 {
			return fmt.Errorf("at most 20 requirements or constraints are allowed per group")
		}
		for _, item := range group {
			if strings.TrimSpace(item) == "" || len(item) > 500 {
				return fmt.Errorf("each requirement or constraint must have 1 to 500 characters")
			}
			total += len(item)
		}
	}
	if total > 6000 {
		return fmt.Errorf("requirements and constraints exceed the 6000-character context budget")
	}
	return nil
}

type resumeIn struct {
	TaskID       string   `json:"task_id" jsonschema:"the task ID returned by bc_task_start"`
	Requirements []string `json:"requirements,omitempty" jsonschema:"explicit user criteria to recover when an older task did not record them at start"`
	Constraints  []string `json:"constraints,omitempty" jsonschema:"explicit user constraints to recover when an older task did not record them at start"`
	Path         string   `json:"path,omitempty" jsonschema:"subdirectory of the open repository"`
}

func openCodeSessionID(req *mcp.CallToolRequest) string {
	if req == nil {
		return ""
	}
	id, _ := req.Params.Meta["ai.opencode/sessionID"].(string)
	if strings.HasPrefix(id, "ses_") && len(id) <= 100 {
		return id
	}
	return ""
}

func (s *Server) taskResume(ctx context.Context, req *mcp.CallToolRequest, in resumeIn) (*mcp.CallToolResult, any, error) {
	if err := validateTaskDetails(in.Requirements, in.Constraints); err != nil {
		return fail("%v", err), nil, nil
	}
	id := openCodeSessionID(req)
	if id == "" {
		return fail("OpenCode session ID is required to resume a task"), nil, nil
	}
	sess, err := s.resolve(ctx, in.Path)
	if err != nil {
		return fail("%v", err), nil, nil
	}
	defer sess.Close()
	t, err := task.NewStore(sess.Store).Get(ctx, in.TaskID)
	if err != nil || t.Kind != "supervised" || t.State.Terminal() {
		return fail("task %q is not an unfinished supervised task", in.TaskID), nil, nil
	}
	if err := supervisor.RecordEvent(ctx, sess.Store, t.ID, ledger.KindSessionStart,
		map[string]any{"objective": t.Title, "executor": "opencode", "session_id": id, "resumed": true,
			"requirements": in.Requirements, "constraints": in.Constraints}); err != nil {
		return fail("recording session resume: %v", err), nil, nil
	}
	return text("Session bound to task " + t.ID + ". Its durable state will appear in subsequent model requests."), nil, nil
}

// ----------------------------------------------------------------- bc_verify

type verifyIn struct {
	TaskID string `json:"task_id,omitempty" jsonschema:"the id bc_task_start returned, so the result is recorded against that task"`
	Level  string `json:"level,omitempty" jsonschema:"low, standard or high. Defaults to standard"`
	Path   string `json:"path,omitempty" jsonschema:"a subdirectory of the open repository; defaults to its root"`
}

type verifyOut struct {
	Accepted   bool     `json:"accepted"`
	Candidate  string   `json:"candidate"`
	Reasons    []string `json:"reasons,omitempty"`
	OutOfScope []string `json:"out_of_scope,omitempty"`
}

// verify puts the working tree under the completion contract.
//
// It is the same path `bcode task verify` takes, through the same assembly in
// internal/supervisor, which is why the runner had to leave cmd/bcode: a second
// construction that forgot the sandbox would still compile and would run the
// repository's test suite unconfined.
//
// The call is slow — minutes on a real repository — and MCP calls are bounded
// by the client's timeout. `bcode opencode setup` writes a generous one for this
// reason, and a client that times out anyway loses the answer rather than the
// work: the journal and the evidence are already written.
func (s *Server) verify(ctx context.Context, _ *mcp.CallToolRequest, in verifyIn) (*mcp.CallToolResult, verifyOut, error) {
	level := recipe.Standard
	if in.Level != "" {
		parsed, ok := recipe.ParseLevel(in.Level)
		if !ok {
			return fail("level must be low, standard or high (got %q)", in.Level), verifyOut{}, nil
		}
		level = parsed
	}
	sess, err := s.resolve(ctx, in.Path)
	if err != nil {
		return fail("%v", err), verifyOut{}, nil
	}
	defer sess.Close() //nolint:contextcheck // cleanup must not take the request context: a cancelled call would then skip closing the databases.

	t := task.Task{
		ID: task.NewID("ocverify"), Title: "verify " + sess.Workspace.Name(),
		Kind: "verification", Verification: level,
		Budget: task.Budget{MaxAttempts: 1, MaxWallTime: 30 * time.Minute},
	}
	if err := task.NewStore(sess.Store).Create(ctx, t); err != nil {
		return fail("opening the verification task: %v", err), verifyOut{}, nil
	}

	// Discard progress: stdout carries the protocol, and this runs inside a
	// tool call where there is nowhere to stream it.
	r, err := supervisor.Runner(ctx, sess.Root, sess.Store, engine.Verify{}, supervisor.Options{
		RepoRoot: sess.Workspace.Root,
	})
	if err != nil {
		return fail("%v", err), verifyOut{}, nil
	}
	// Judge what the developer is actually looking at, not the last commit.
	r.SyncUncommitted = true

	outcome, err := r.Run(ctx, t.ID, sess.Workspace.Root)
	if err != nil {
		return fail("verification could not run: %v", err), verifyOut{}, nil
	}
	// Attach the result to the supervised task, so the final review reports
	// what was actually checked rather than what the agent says it checked.
	// Without this the chain breaks silently: every task would finish
	// UNVERIFIED however many times it had passed.
	if in.TaskID != "" {
		if err := supervisor.RecordVerification(ctx, sess.Store, in.TaskID, outcome); err != nil {
			return fail("recording the verification against %s: %v", in.TaskID, err), verifyOut{}, nil
		}
	}
	return text(renderOutcome(outcome)), verifyOut{
		Accepted:   outcome.Accepted,
		Candidate:  outcome.Candidate,
		Reasons:    outcome.Reasons,
		OutOfScope: outcome.OutOfScope,
	}, nil
}

func renderOutcome(o *task.Outcome) string {
	var b strings.Builder
	verdict := "NOT ACCEPTED"
	if o.Accepted {
		verdict = "ACCEPTED"
	}
	fmt.Fprintf(&b, "%s — candidate %s\n\n", verdict, short(o.Candidate))
	for _, res := range o.Results {
		fmt.Fprintf(&b, "  %-16s %-9s %s\n", res.Recipe, res.Status, res.Summary.Headline)
		// The findings are the part an agent can act on. A headline says a test
		// failed; a finding says which one and where.
		for _, f := range res.Summary.Findings {
			if f.File != "" {
				fmt.Fprintf(&b, "      %s:%d  %s\n", f.File, f.Line, f.Message)
				continue
			}
			fmt.Fprintf(&b, "      %s\n", f.Message)
		}
	}
	if len(o.OutOfScope) > 0 {
		b.WriteString("\nChanged outside the declared scope:\n")
		for _, p := range o.OutOfScope {
			fmt.Fprintf(&b, "  %s\n", p)
		}
	}
	if len(o.Reasons) > 0 {
		b.WriteString("\n")
		for _, r := range o.Reasons {
			fmt.Fprintf(&b, "%s\n", r)
		}
	}
	if !o.Accepted {
		b.WriteString("\nFix what failed above and call bc_verify again. Do not report the " +
			"work as finished until this says ACCEPTED.\n")
	}
	return b.String()
}

func short(hash string) string {
	if len(hash) > 12 {
		return hash[:12]
	}
	return hash
}
