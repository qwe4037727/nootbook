-- M2 world/KB extension schema (per-project database: <project>/agent.db).
-- Roles and settings remain in knowledge.json; faction/location/item live here.
-- All rows are project-scoped by construction (one database per novel project).

CREATE TABLE IF NOT EXISTS kb_entry (
    id               TEXT PRIMARY KEY,
    entry_kind       TEXT NOT NULL,
    name             TEXT NOT NULL,
    aliases_json     TEXT NOT NULL DEFAULT '[]',
    summary          TEXT NOT NULL DEFAULT '',
    fields_json      TEXT NOT NULL DEFAULT '{}',
    source_refs_json TEXT NOT NULL DEFAULT '[]',
    confidence       REAL NOT NULL DEFAULT 0,
    last_seen        INTEGER NOT NULL DEFAULT 0,
    created_at       INTEGER NOT NULL,
    updated_at       INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_kb_entry_kind ON kb_entry(entry_kind, updated_at);
CREATE INDEX IF NOT EXISTS idx_kb_entry_name ON kb_entry(name);

CREATE TABLE IF NOT EXISTS kb_field_lock (
    entry_id   TEXT NOT NULL,
    field      TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    PRIMARY KEY (entry_id, field)
);

CREATE TABLE IF NOT EXISTS kb_relation (
    id                TEXT PRIMARY KEY,
    from_entry_id     TEXT NOT NULL,
    to_entry_id       TEXT NOT NULL,
    relation_type     TEXT NOT NULL,
    label             TEXT NOT NULL DEFAULT '',
    description       TEXT NOT NULL DEFAULT '',
    status            TEXT NOT NULL DEFAULT 'active',
    start_chapter     INTEGER NOT NULL DEFAULT 0,
    end_chapter       INTEGER NOT NULL DEFAULT 0,
    source_refs_json  TEXT NOT NULL DEFAULT '[]',
    created_at        INTEGER NOT NULL,
    updated_at        INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_kb_relation_from ON kb_relation(from_entry_id, status);
CREATE INDEX IF NOT EXISTS idx_kb_relation_to ON kb_relation(to_entry_id, status);
CREATE INDEX IF NOT EXISTS idx_kb_relation_chapter ON kb_relation(start_chapter, end_chapter);

CREATE TABLE IF NOT EXISTS kb_timeline_event (
    id               TEXT PRIMARY KEY,
    chapter_id       TEXT NOT NULL DEFAULT '',
    chapter_index    INTEGER NOT NULL DEFAULT 0,
    title            TEXT NOT NULL,
    summary          TEXT NOT NULL DEFAULT '',
    event_type       TEXT NOT NULL DEFAULT 'other',
    participants_json TEXT NOT NULL DEFAULT '[]',
    entities_json    TEXT NOT NULL DEFAULT '[]',
    source           TEXT NOT NULL DEFAULT 'manual',
    source_event_id  TEXT NOT NULL DEFAULT '',
    story_order      INTEGER NOT NULL DEFAULT 0,
    created_at       INTEGER NOT NULL,
    updated_at       INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_kb_timeline_chapter ON kb_timeline_event(chapter_index, story_order);
CREATE INDEX IF NOT EXISTS idx_kb_timeline_source_event ON kb_timeline_event(source_event_id);
