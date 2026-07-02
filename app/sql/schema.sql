CREATE TABLE IF NOT EXISTS cameras (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    host TEXT NOT NULL,
    port INTEGER NOT NULL DEFAULT 80,
    username TEXT NOT NULL DEFAULT '',
    password TEXT NOT NULL DEFAULT '',
    profile_token TEXT,
    interval_seconds INTEGER NOT NULL DEFAULT 60,
    snapshot_url TEXT,
    enabled BOOLEAN NOT NULL DEFAULT 1,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS snapshots (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    camera_id INTEGER NOT NULL,
    captured_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    image_path TEXT NOT NULL,
    file_size INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'success',
    error_message TEXT,
    FOREIGN KEY (camera_id) REFERENCES cameras(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_snapshots_camera_date ON snapshots(camera_id, captured_at);
CREATE INDEX IF NOT EXISTS idx_snapshots_captured_at ON snapshots(captured_at);

CREATE TABLE IF NOT EXISTS analysis_jobs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    snapshot_id INTEGER NOT NULL,
    job_type TEXT NOT NULL DEFAULT 'yolo_detection',
    status TEXT NOT NULL DEFAULT 'pending',
    priority INTEGER NOT NULL DEFAULT 0,
    attempts INTEGER NOT NULL DEFAULT 0,
    max_attempts INTEGER NOT NULL DEFAULT 3,
    error_message TEXT,
    requested_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    started_at TIMESTAMP,
    finished_at TIMESTAMP,
    FOREIGN KEY (snapshot_id) REFERENCES snapshots(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_analysis_jobs_status ON analysis_jobs(status, priority);
CREATE INDEX IF NOT EXISTS idx_analysis_jobs_snapshot ON analysis_jobs(snapshot_id);

CREATE TABLE IF NOT EXISTS snapshot_analyses (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    snapshot_id INTEGER NOT NULL,
    model_name TEXT NOT NULL,
    model_version TEXT NOT NULL DEFAULT '1.0',
    status TEXT NOT NULL DEFAULT 'pending',
    objects_json TEXT,
    person_count INTEGER NOT NULL DEFAULT 0,
    review_required BOOLEAN NOT NULL DEFAULT 0,
    review_reason TEXT,
    anomaly_score REAL,
    error_message TEXT,
    analyzed_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (snapshot_id) REFERENCES snapshots(id) ON DELETE CASCADE,
    UNIQUE(snapshot_id, model_name)
);

CREATE INDEX IF NOT EXISTS idx_analyses_review ON snapshot_analyses(review_required, status);
