-- M3 adaptation workbench audit schema (per-project database: <project>/agent.db).
-- Rewrite results never write memory directly; this table only records audit/cost.

CREATE TABLE IF NOT EXISTS adapt_run (
    id                TEXT PRIMARY KEY,
    chapter_id        TEXT NOT NULL,
    chapter_index     INTEGER NOT NULL DEFAULT 0,
    mode              TEXT NOT NULL,
    instruction_type  TEXT NOT NULL DEFAULT '',
    instruction       TEXT NOT NULL DEFAULT '',
    start_offset      INTEGER NOT NULL DEFAULT 0,
    end_offset        INTEGER NOT NULL DEFAULT 0,
    old_chars         INTEGER NOT NULL DEFAULT 0,
    new_chars         INTEGER NOT NULL DEFAULT 0,
    style             TEXT NOT NULL DEFAULT '',
    perspective       TEXT NOT NULL DEFAULT '',
    model             TEXT NOT NULL DEFAULT '',
    prompt_tokens     INTEGER NOT NULL DEFAULT 0,
    completion_tokens INTEGER NOT NULL DEFAULT 0,
    total_tokens      INTEGER NOT NULL DEFAULT 0,
    draft_id          TEXT NOT NULL DEFAULT '',
    impact_json       TEXT NOT NULL DEFAULT '{}',
    status            TEXT NOT NULL DEFAULT 'done',
    error             TEXT NOT NULL DEFAULT '',
    created_at        INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_adapt_run_chapter ON adapt_run(chapter_id, created_at);
CREATE INDEX IF NOT EXISTS idx_adapt_run_created ON adapt_run(created_at);
