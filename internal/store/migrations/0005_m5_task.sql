-- M5 task scheduling center schema (per-project database: <project>/agent.db).
-- Tasks are persisted so pause/cancel/restart recovery survive process exits.

CREATE TABLE IF NOT EXISTS task (
    id           TEXT PRIMARY KEY,
    project_id   TEXT NOT NULL DEFAULT '',
    kind         TEXT NOT NULL,
    description  TEXT NOT NULL DEFAULT '',
    priority     TEXT NOT NULL DEFAULT 'normal',
    status       TEXT NOT NULL DEFAULT 'pending',
    params_json  TEXT NOT NULL DEFAULT '{}',
    progress     INTEGER NOT NULL DEFAULT 0,
    total        INTEGER NOT NULL DEFAULT 0,
    result_json  TEXT NOT NULL DEFAULT '{}',
    error        TEXT NOT NULL DEFAULT '',
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL,
    started_at   INTEGER NOT NULL DEFAULT 0,
    finished_at  INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_task_status ON task(status, priority, created_at);
CREATE INDEX IF NOT EXISTS idx_task_created ON task(created_at DESC);
