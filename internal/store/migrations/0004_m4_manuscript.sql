-- M4 manuscript enhancement schema (per-project database: <project>/agent.db).
-- Chapter versions / whole-book snapshots / multi-candidate continuation audit.
-- Snapshots and versions are audit artifacts: restoring them never bypasses
-- the store lock order (store.mu -> memoryMu) and always emits change events.

CREATE TABLE IF NOT EXISTS chapter_version (
    id           TEXT PRIMARY KEY,
    chapter_id   TEXT NOT NULL,
    chapter_index INTEGER NOT NULL DEFAULT 0,
    revision     INTEGER NOT NULL DEFAULT 0,
    title        TEXT NOT NULL DEFAULT '',
    content      TEXT NOT NULL DEFAULT '',
    char_count   INTEGER NOT NULL DEFAULT 0,
    reason       TEXT NOT NULL DEFAULT '',
    created_at   INTEGER NOT NULL,
    UNIQUE(chapter_id, revision)
);

CREATE INDEX IF NOT EXISTS idx_chapter_version_chapter ON chapter_version(chapter_id, revision DESC);

CREATE TABLE IF NOT EXISTS book_snapshot (
    id             TEXT PRIMARY KEY,
    name           TEXT NOT NULL DEFAULT '',
    reason         TEXT NOT NULL DEFAULT 'manual',
    title          TEXT NOT NULL DEFAULT '',
    chapter_count  INTEGER NOT NULL DEFAULT 0,
    char_count     INTEGER NOT NULL DEFAULT 0,
    knowledge_json TEXT NOT NULL DEFAULT '{}',
    created_at     INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_book_snapshot_created ON book_snapshot(created_at DESC);

CREATE TABLE IF NOT EXISTS book_snapshot_chapter (
    snapshot_id  TEXT NOT NULL,
    chapter_id   TEXT NOT NULL,
    index_no     INTEGER NOT NULL,
    title        TEXT NOT NULL DEFAULT '',
    content      TEXT NOT NULL DEFAULT '',
    char_count   INTEGER NOT NULL DEFAULT 0,
    revision     INTEGER NOT NULL DEFAULT 1,
    PRIMARY KEY (snapshot_id, chapter_id)
);

CREATE INDEX IF NOT EXISTS idx_book_snapshot_chapter_order ON book_snapshot_chapter(snapshot_id, index_no);

CREATE TABLE IF NOT EXISTS continue_group (
    id              TEXT PRIMARY KEY,
    start_mode      TEXT NOT NULL DEFAULT 'full',
    chapter_id      TEXT NOT NULL DEFAULT '',
    start_offset    INTEGER NOT NULL DEFAULT 0,
    start_index     INTEGER NOT NULL DEFAULT 0,
    direction       TEXT NOT NULL DEFAULT '',
    target_chars    INTEGER NOT NULL DEFAULT 0,
    candidate_count INTEGER NOT NULL DEFAULT 1,
    temperature     REAL NOT NULL DEFAULT 0,
    model           TEXT NOT NULL DEFAULT '',
    prompt_chars    INTEGER NOT NULL DEFAULT 0,
    context_chars   INTEGER NOT NULL DEFAULT 0,
    memory_chars    INTEGER NOT NULL DEFAULT 0,
    created_at      INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_continue_group_created ON continue_group(created_at DESC);

CREATE TABLE IF NOT EXISTS continue_candidate (
    id                TEXT PRIMARY KEY,
    group_id          TEXT NOT NULL,
    index_no          INTEGER NOT NULL,
    content           TEXT NOT NULL DEFAULT '',
    char_count        INTEGER NOT NULL DEFAULT 0,
    temperature       REAL NOT NULL DEFAULT 0,
    prompt_tokens     INTEGER NOT NULL DEFAULT 0,
    completion_tokens INTEGER NOT NULL DEFAULT 0,
    total_tokens      INTEGER NOT NULL DEFAULT 0,
    status            TEXT NOT NULL DEFAULT 'done',
    error             TEXT NOT NULL DEFAULT '',
    created_at        INTEGER NOT NULL,
    UNIQUE(group_id, index_no)
);

CREATE INDEX IF NOT EXISTS idx_continue_candidate_group ON continue_candidate(group_id, index_no);
