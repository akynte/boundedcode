package store

// migrations are applied in order; never edit a released migration, append a
// new one instead.
var migrations = []string{
	// 1: core state.
	`
CREATE TABLE workspaces (
	id          TEXT PRIMARY KEY,
	name        TEXT NOT NULL UNIQUE,
	root        TEXT NOT NULL DEFAULT '',
	created_at  TEXT NOT NULL,
	updated_at  TEXT NOT NULL
);

CREATE TABLE repositories (
	id            TEXT PRIMARY KEY,
	workspace_id  TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
	name          TEXT NOT NULL,
	path          TEXT NOT NULL,
	origin        TEXT NOT NULL DEFAULT '',
	default_branch TEXT NOT NULL DEFAULT '',
	languages     TEXT NOT NULL DEFAULT '[]',
	index_project TEXT NOT NULL DEFAULT '',
	indexed_at    TEXT NOT NULL DEFAULT '',
	created_at    TEXT NOT NULL,
	UNIQUE(workspace_id, name),
	UNIQUE(workspace_id, path)
);

CREATE TABLE tasks (
	id                 TEXT PRIMARY KEY,
	workspace_id       TEXT NOT NULL REFERENCES workspaces(id),
	original_request   TEXT NOT NULL,
	goal               TEXT NOT NULL DEFAULT '',
	acceptance_criteria TEXT NOT NULL DEFAULT '[]',
	status             TEXT NOT NULL,
	phase              TEXT NOT NULL,
	completed_steps    TEXT NOT NULL DEFAULT '[]',
	remaining_steps    TEXT NOT NULL DEFAULT '[]',
	attempt_count      INTEGER NOT NULL DEFAULT 0,
	decisions          TEXT NOT NULL DEFAULT '[]',
	verification_state TEXT NOT NULL DEFAULT '',
	changed_repositories TEXT NOT NULL DEFAULT '[]',
	changed_files      TEXT NOT NULL DEFAULT '[]',
	changed_symbols    TEXT NOT NULL DEFAULT '[]',
	agent_runtime      TEXT NOT NULL DEFAULT '',
	agent_session_id   TEXT NOT NULL DEFAULT '',
	model_profile      TEXT NOT NULL DEFAULT '',
	budget             TEXT NOT NULL DEFAULT '{}',
	created_at         TEXT NOT NULL,
	updated_at         TEXT NOT NULL,
	finished_at        TEXT NOT NULL DEFAULT ''
);
CREATE INDEX tasks_workspace ON tasks(workspace_id);

CREATE TABLE task_worktrees (
	task_id       TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
	repository_id TEXT NOT NULL REFERENCES repositories(id),
	path          TEXT NOT NULL,
	branch        TEXT NOT NULL,
	base_commit   TEXT NOT NULL,
	PRIMARY KEY(task_id, repository_id)
);

CREATE TABLE strategies (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	task_id     TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
	attempt     INTEGER NOT NULL,
	summary     TEXT NOT NULL,
	outcome     TEXT NOT NULL DEFAULT 'active',  -- active | succeeded | rejected
	reason      TEXT NOT NULL DEFAULT '',
	created_at  TEXT NOT NULL
);

CREATE TABLE verification_runs (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	task_id     TEXT NOT NULL DEFAULT '',
	repository  TEXT NOT NULL,
	scope       TEXT NOT NULL,            -- targeted | full
	passed      INTEGER NOT NULL,
	stages      TEXT NOT NULL,            -- JSON []StageResult
	started_at  TEXT NOT NULL,
	duration_ms INTEGER NOT NULL
);
CREATE INDEX verification_runs_task ON verification_runs(task_id);

CREATE TABLE escalations (
	id           INTEGER PRIMARY KEY AUTOINCREMENT,
	task_id      TEXT NOT NULL DEFAULT '',
	trigger      TEXT NOT NULL,            -- Z1 | Z2 | Z3 | Z4
	reason       TEXT NOT NULL,
	provider     TEXT NOT NULL,
	status       TEXT NOT NULL,            -- proposed | approved | declined | sent | answered | failed
	packet_path  TEXT NOT NULL DEFAULT '',
	packet_tokens INTEGER NOT NULL DEFAULT 0,
	response_path TEXT NOT NULL DEFAULT '',
	outcome      TEXT NOT NULL DEFAULT '', -- helped | no_effect | unknown
	created_at   TEXT NOT NULL,
	updated_at   TEXT NOT NULL
);

CREATE TABLE model_calls (
	id                INTEGER PRIMARY KEY AUTOINCREMENT,
	task_id           TEXT NOT NULL DEFAULT '',
	source            TEXT NOT NULL,       -- agent | planner | benchmark
	model             TEXT NOT NULL,
	prompt_tokens     INTEGER NOT NULL DEFAULT 0,
	completion_tokens INTEGER NOT NULL DEFAULT 0,
	cached_tokens     INTEGER NOT NULL DEFAULT 0,
	prompt_ms         REAL NOT NULL DEFAULT 0,
	decode_ms         REAL NOT NULL DEFAULT 0,
	total_ms          REAL NOT NULL DEFAULT 0,
	status            TEXT NOT NULL,
	created_at        TEXT NOT NULL
);
CREATE INDEX model_calls_task ON model_calls(task_id);

CREATE TABLE events (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	ts         TEXT NOT NULL,
	task_id    TEXT NOT NULL DEFAULT '',
	kind       TEXT NOT NULL,
	data       TEXT NOT NULL DEFAULT '{}'
);
CREATE INDEX events_task ON events(task_id, id);

CREATE TABLE benchmark_runs (
	id          TEXT PRIMARY KEY,
	kind        TEXT NOT NULL,             -- infra | engineering
	model       TEXT NOT NULL,
	config      TEXT NOT NULL,
	result      TEXT NOT NULL,
	machine     TEXT NOT NULL,
	created_at  TEXT NOT NULL
);
`,
	// 2: cross-service contract endpoints (internal/xservice), refreshed per
	// repository on index.
	`
CREATE TABLE xservice_endpoints (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	workspace_id  TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
	repository_id TEXT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
	repo          TEXT NOT NULL,
	kind          TEXT NOT NULL,
	file          TEXT NOT NULL,
	line          INTEGER NOT NULL,
	symbol        TEXT NOT NULL DEFAULT '',
	method        TEXT NOT NULL DEFAULT '',
	path          TEXT NOT NULL DEFAULT '',
	topic         TEXT NOT NULL DEFAULT '',
	env           TEXT NOT NULL DEFAULT '',
	confidence    TEXT NOT NULL,
	detail        TEXT NOT NULL DEFAULT '',
	scanned_at    TEXT NOT NULL
);
CREATE INDEX xservice_endpoints_ws ON xservice_endpoints(workspace_id);
CREATE INDEX xservice_endpoints_repo ON xservice_endpoints(repository_id);
`,
}
