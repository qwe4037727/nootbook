-- M6 image assets (per-project database: <project>/agent.db).
-- One row per generation request; pending/running rows double as the image job queue.

CREATE TABLE IF NOT EXISTS image_asset (
    id                  TEXT PRIMARY KEY,
    kind                TEXT NOT NULL DEFAULT 'illustration',
    entry_id            TEXT NOT NULL DEFAULT '',
    chapter_id          TEXT NOT NULL DEFAULT '',
    anchor_offset       INTEGER NOT NULL DEFAULT 0,
    file_key            TEXT NOT NULL DEFAULT '',
    thumb_key           TEXT NOT NULL DEFAULT '',
    prompt_json         TEXT NOT NULL DEFAULT '{}',
    features_json       TEXT NOT NULL DEFAULT '[]',
    core_features_json  TEXT NOT NULL DEFAULT '[]',
    model               TEXT NOT NULL DEFAULT '',
    backend             TEXT NOT NULL DEFAULT '',
    style_tag           TEXT NOT NULL DEFAULT '',
    negative_prompt     TEXT NOT NULL DEFAULT '',
    width               INTEGER NOT NULL DEFAULT 0,
    height              INTEGER NOT NULL DEFAULT 0,
    steps               INTEGER NOT NULL DEFAULT 0,
    seed                INTEGER NOT NULL DEFAULT 0,
    is_anchor           INTEGER NOT NULL DEFAULT 0,
    parent_asset        TEXT NOT NULL DEFAULT '',
    variant_of          TEXT NOT NULL DEFAULT '',
    consistency_score   REAL NOT NULL DEFAULT 0,
    consistency_note    TEXT NOT NULL DEFAULT '',
    visual_proxy        INTEGER NOT NULL DEFAULT 1,
    status              TEXT NOT NULL DEFAULT 'pending',
    error               TEXT NOT NULL DEFAULT '',
    duration_ms         INTEGER NOT NULL DEFAULT 0,
    cost_usd            REAL NOT NULL DEFAULT 0,
    created_at          INTEGER NOT NULL,
    updated_at          INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_image_asset_status ON image_asset(status, created_at);
CREATE INDEX IF NOT EXISTS idx_image_asset_entry ON image_asset(kind, entry_id);
CREATE INDEX IF NOT EXISTS idx_image_asset_chapter ON image_asset(chapter_id);
CREATE INDEX IF NOT EXISTS idx_image_asset_anchor ON image_asset(is_anchor, kind, entry_id);
