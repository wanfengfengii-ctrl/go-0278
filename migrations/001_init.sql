-- Initial schema for the rock-bolt pullout inspection service.
-- Constraints mirror the documented uniqueness rules: canonical hole keys and
-- layer ownership are unique per task, device lease windows are protected
-- against overlap, verdict credentials are unique per task, and reinforcements
-- enforce a strictly monotonic generation per task.

CREATE TABLE IF NOT EXISTS inspection_tasks (
    id                  TEXT PRIMARY KEY,
    mileage_start_mm    INTEGER NOT NULL,
    mileage_end_mm      INTEGER NOT NULL,
    rock_grade          TEXT    NOT NULL,
    cycle_no            INTEGER NOT NULL,
    layout_revision     TEXT    NOT NULL DEFAULT '',
    layout_digest       TEXT    NOT NULL DEFAULT '',
    grout_digest        TEXT    NOT NULL DEFAULT '',
    sampling_seed       INTEGER NOT NULL,
    generation          INTEGER NOT NULL,
    status              TEXT    NOT NULL,
    version             INTEGER NOT NULL DEFAULT 0,
    locked_at           INTEGER,
    verdict             TEXT,
    verdict_credential  TEXT    NOT NULL DEFAULT '',
    seal_digest         TEXT    NOT NULL DEFAULT '',
    layer_quotas        TEXT    NOT NULL DEFAULT '{}',
    load_levels         TEXT    NOT NULL DEFAULT '[]',
    stop_threshold      INTEGER NOT NULL DEFAULT 0,
    accept_threshold    INTEGER NOT NULL DEFAULT 0,
    influence_bound     TEXT    NOT NULL DEFAULT '',
    calibration_digest  TEXT    NOT NULL DEFAULT '',
    lock_digest         TEXT    NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS candidate_holes (
    task_id      TEXT   NOT NULL REFERENCES inspection_tasks(id),
    mileage_mm   INTEGER NOT NULL,
    ring_no      INTEGER NOT NULL,
    azimuth      TEXT   NOT NULL,
    hole_no      INTEGER NOT NULL,
    coord_x      INTEGER NOT NULL DEFAULT 0,
    coord_y      INTEGER NOT NULL DEFAULT 0,
    coord_z      INTEGER NOT NULL DEFAULT 0,
    bar_batch    TEXT   NOT NULL,
    anchor_batch TEXT   NOT NULL,
    layer_key    TEXT   NOT NULL,
    UNIQUE (task_id, mileage_mm, ring_no, azimuth, hole_no)
);

CREATE TABLE IF NOT EXISTS sample_nodes (
    id               TEXT PRIMARY KEY,
    task_id          TEXT    NOT NULL REFERENCES inspection_tasks(id),
    parent_id        TEXT    NOT NULL DEFAULT '',
    layer_key        TEXT    NOT NULL DEFAULT '',
    hole_mileage     INTEGER,
    hole_ring        INTEGER,
    hole_azimuth     TEXT,
    hole_no          INTEGER,
    category         TEXT    NOT NULL,
    pick_order       INTEGER NOT NULL,
    source_failure   TEXT    NOT NULL DEFAULT '',
    influence_digest TEXT    NOT NULL DEFAULT '',
    generation       INTEGER NOT NULL,
    closed           INTEGER NOT NULL DEFAULT 0,
    verified         INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS devices (
    device_no   TEXT PRIMARY KEY,
    device_type TEXT NOT NULL,
    state       TEXT NOT NULL DEFAULT 'available',
    calib_ver   TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS device_leases (
    id          TEXT PRIMARY KEY,
    device_no   TEXT NOT NULL,
    test_id     TEXT NOT NULL,
    generation  INTEGER NOT NULL,
    start_at    INTEGER NOT NULL,
    end_at      INTEGER NOT NULL,
    version     INTEGER NOT NULL DEFAULT 0,
    released_at INTEGER
);

CREATE TABLE IF NOT EXISTS load_evidence (
    seq                  INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id              TEXT    NOT NULL,
    sample_id            TEXT    NOT NULL,
    generation           INTEGER NOT NULL,
    kind                 TEXT    NOT NULL,
    stage                TEXT    NOT NULL,
    load_level           INTEGER NOT NULL,
    load                 INTEGER NOT NULL,
    displacement         INTEGER NOT NULL,
    hold_secs            INTEGER NOT NULL,
    rebound              INTEGER NOT NULL,
    ratio                INTEGER NOT NULL,
    device_puller        TEXT    NOT NULL DEFAULT '',
    device_pump          TEXT    NOT NULL DEFAULT '',
    device_displacement  TEXT    NOT NULL DEFAULT '',
    accepted             INTEGER NOT NULL,
    content_digest       TEXT    NOT NULL
);

CREATE TABLE IF NOT EXISTS instrument_calls (
    call_key       TEXT PRIMARY KEY,
    task_id        TEXT    NOT NULL,
    sample_id      TEXT    NOT NULL,
    generation     INTEGER NOT NULL,
    load_level     INTEGER NOT NULL,
    device_no      TEXT    NOT NULL,
    seq            INTEGER NOT NULL,
    result         TEXT    NOT NULL,
    retry_count    INTEGER NOT NULL,
    next_retry_at  INTEGER NOT NULL,
    evidence_ref   INTEGER,
    request_digest TEXT    NOT NULL,
    stage          TEXT    NOT NULL DEFAULT '',
    load           INTEGER NOT NULL DEFAULT 0,
    displacement   INTEGER NOT NULL DEFAULT 0,
    hold_secs      INTEGER NOT NULL DEFAULT 0,
    rebound        INTEGER NOT NULL DEFAULT 0,
    device_puller  TEXT    NOT NULL DEFAULT '',
    device_pump    TEXT    NOT NULL DEFAULT '',
    device_disp    TEXT    NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS reinforcements (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id           TEXT    NOT NULL REFERENCES inspection_tasks(id),
    influence_digest  TEXT    NOT NULL,
    reinforce_digest  TEXT    NOT NULL,
    prev_generation   INTEGER NOT NULL,
    new_generation    INTEGER NOT NULL,
    reason            TEXT    NOT NULL,
    effective_at      INTEGER NOT NULL,
    UNIQUE (task_id, new_generation)
);

CREATE TABLE IF NOT EXISTS reviews (
    task_id          TEXT    NOT NULL REFERENCES inspection_tasks(id),
    reviewer         TEXT    NOT NULL,
    qual_snapshot    TEXT    NOT NULL,
    signature_digest TEXT    NOT NULL,
    submit_version   INTEGER NOT NULL,
    PRIMARY KEY (task_id, reviewer)
);

CREATE TABLE IF NOT EXISTS operation_records (
    operation_id    TEXT PRIMARY KEY,
    task_id         TEXT NOT NULL,
    action          TEXT NOT NULL,
    request_digest  TEXT NOT NULL,
    response_code   TEXT NOT NULL,
    response_digest TEXT NOT NULL,
    commit_version  INTEGER NOT NULL
);
