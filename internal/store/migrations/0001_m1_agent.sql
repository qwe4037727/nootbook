-- M1 agent cluster schema (per-project database: <project>/agent.db).
-- All rows are project-scoped by construction (one database per novel project).

CREATE TABLE IF NOT EXISTS agent_run (
    id                TEXT PRIMARY KEY,
    project_id        TEXT NOT NULL,
    agent_key         TEXT NOT NULL,
    agent_name        TEXT NOT NULL,
    event_id          TEXT NOT NULL,
    event_type        TEXT NOT NULL,
    permission        TEXT NOT NULL,
    status            TEXT NOT NULL,
    started_at        INTEGER NOT NULL,
    finished_at       INTEGER NOT NULL,
    duration_ms       INTEGER NOT NULL DEFAULT 0,
    prompt_tokens     INTEGER NOT NULL DEFAULT 0,
    completion_tokens INTEGER NOT NULL DEFAULT 0,
    total_tokens      INTEGER NOT NULL DEFAULT 0,
    input_chars       INTEGER NOT NULL DEFAULT 0,
    output_chars      INTEGER NOT NULL DEFAULT 0,
    summary           TEXT NOT NULL DEFAULT '',
    error             TEXT NOT NULL DEFAULT '',
    report_id         TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_agent_run_agent ON agent_run(agent_key, started_at);
CREATE INDEX IF NOT EXISTS idx_agent_run_event ON agent_run(event_id);

CREATE TABLE IF NOT EXISTS foreshadow (
    id            TEXT PRIMARY KEY,
    chapter_id    TEXT NOT NULL,
    chapter_index INTEGER NOT NULL DEFAULT 0,
    title         TEXT NOT NULL DEFAULT '',
    kind          TEXT NOT NULL DEFAULT '',
    quote         TEXT NOT NULL DEFAULT '',
    status        TEXT NOT NULL DEFAULT 'candidate',
    confidence    REAL NOT NULL DEFAULT 0,
    related_json  TEXT NOT NULL DEFAULT '[]',
    evidence_note TEXT NOT NULL DEFAULT '',
    created_at    INTEGER NOT NULL,
    updated_at    INTEGER NOT NULL,
    UNIQUE(chapter_id, quote)
);

CREATE INDEX IF NOT EXISTS idx_foreshadow_status ON foreshadow(status, chapter_index);

CREATE TABLE IF NOT EXISTS agent_report (
    id             TEXT PRIMARY KEY,
    agent_key      TEXT NOT NULL,
    agent_name     TEXT NOT NULL,
    event_id       TEXT NOT NULL DEFAULT '',
    chapter_id     TEXT NOT NULL DEFAULT '',
    chapter_index  INTEGER NOT NULL DEFAULT 0,
    severity       TEXT NOT NULL DEFAULT 'info',
    summary        TEXT NOT NULL DEFAULT '',
    findings_json  TEXT NOT NULL DEFAULT '[]',
    created_at     INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_agent_report_agent ON agent_report(agent_key, created_at);
CREATE INDEX IF NOT EXISTS idx_agent_report_chapter ON agent_report(chapter_id);

CREATE TABLE IF NOT EXISTS agent_proposal (
    id           TEXT PRIMARY KEY,
    agent_key    TEXT NOT NULL,
    kind         TEXT NOT NULL,
    target_id    TEXT NOT NULL DEFAULT '',
    payload_json TEXT NOT NULL DEFAULT '{}',
    summary      TEXT NOT NULL DEFAULT '',
    risk         TEXT NOT NULL DEFAULT 'low',
    status       TEXT NOT NULL DEFAULT 'pending',
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_agent_proposal_status ON agent_proposal(status, created_at);

CREATE TABLE IF NOT EXISTS agent_config (
    agent_key  TEXT PRIMARY KEY,
    enabled    INTEGER NOT NULL DEFAULT 1,
    level      TEXT NOT NULL DEFAULT 'observe',
    updated_at INTEGER NOT NULL
);
