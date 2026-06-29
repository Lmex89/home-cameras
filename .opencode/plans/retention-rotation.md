# Data Retention & Rotation Plan

## Goal

Implement automated rotation for snapshots and videos: compress old snapshots to save space while keeping them accessible from the dashboard, and delete everything past retention period.

## Data lifecycle

```
raw file          → zipped archive       → deleted
(snapshot/video captured)  (> 7 days)    (> 30 days)
```

## Files to modify

### 1. `app/core/config.py` — new settings

```python
snapshot_zip_after_days: int = 7     # after this many days, raw files → zip
snapshot_retention_days: int = 30    # delete zipped archives older than this
video_retention_days: int = 30       # delete mp4 files older than this
```

### 2. Domain model — `app/domain/models.py`

Add column to `Snapshot`:

```python
archive_path: Mapped[str | None] = mapped_column(Text, nullable=True)
```

This stores the relative path inside the zip (e.g. `3/2025/06/15/143022.jpg`) so the dashboard can reconstruct the archive path and extract on demand.

### 3. Raw DDL — `app/sql/schema.sql`

Add column:

```sql
ALTER TABLE snapshots ADD COLUMN archive_path TEXT;
```

(Pragma: we already run schema.sql on startup, but since `CREATE TABLE IF NOT EXISTS` won't add columns, we need a migration check similar to the existing `_migrate_interval_column`.)

### 4. New file — `app/application/services/retention_service.py`

Contains `RetentionService` with methods:

#### `zip_old_snapshots(cutoff_date) -> int`
- Query snapshots WHERE `captured_at < cutoff_date` AND `archive_path IS NULL`
- Group by `(camera_id, captured_at.date)` → one zip per camera/day
- Zip file at: `data/archives/{camera_id}/{date}.zip`
- Update each snapshot's `archive_path` to e.g. `{camera_id}/{date}.zip::{relative_path_in_zip}`
- Delete raw JPG file from disk
- Return count of processed snapshots

#### `zip_old_videos(cutoff_date) -> int`
- List `data/videos/*.mp4` whose filename date < cutoff_date AND not already zipped
- Group by camera id and date from filename → `data/archives/videos/{camera_id}/{date}.zip`
- Delete raw MP4 after archiving
- Return count

#### `delete_expired_snapshots(cutoff_date) -> int`
- Query snapshots WHERE `captured_at < cutoff_date`
- For each: delete the archive zip file (if all snapshots in that zip are expired)
- Delete the DB row
- Return count

#### `delete_expired_videos(cutoff_date) -> int`
- List `data/archives/videos/*.zip` older than retention
- Delete files
- Also delete any stray MP4 files (missed by zip step)
- Return count

#### `run() -> dict`
- Orchestrate all four steps
- Log summary at INFO level
- Run PRAGMA optimize after large deletes

### 5. Snapshot serving — `app/api/routers/snapshots.py`

Modify snapshot serving to handle archived snapshots:

- When `archive_path` is set, parse the zip path + internal file path
- Open the zip, extract the specific file, return as `Response` with `image/jpeg`
- Optionally cache extracted files to a temp directory

Alternative: add a new endpoint `GET /api/snapshots/{id}/image` that checks `archive_path` and serves from zip or file.

### 6. Scheduler — `app/scheduler.py`

Add a retention job:

```python
async def retention_job():
    async with UnitOfWork(session_factory) as uow:
        service = RetentionService(uow)
        result = await service.run()
        logger.info(f"Retention cleanup: {result}")
```

Schedule it daily at 03:00:

```python
scheduler.add_job(
    retention_job,
    trigger=CronTrigger(hour=3, minute=0),
    id="retention_cleanup",
    replace_existing=True,
)
```

### 7. Lifespan — `app/main.py`

Schedule the retention job on startup (alongside loading capture schedule):

```python
# after scheduler.start()
scheduler.add_job(retention_job, CronTrigger(hour=3, minute=0), id="retention_cleanup")
```

### 8. Dashboard — `index.html`

Check for archived snapshot images: if the image fetch fails (404), the archived endpoint can serve from zip transparently. No frontend change strictly required if the backend serves archived images at the same URL pattern.

### 9. Example `.env` / Docker Compose

Update `.env.example` and `docker-compose.yml` with new vars:

```
SNAPSHOT_RETENTION_DAYS=30
SNAPSHOT_ZIP_AFTER_DAYS=7
VIDEO_RETENTION_DAYS=30
```

## Archive ZIP structure

```
data/archives/
  ├── snapshots/
  │   └── {camera_id}/
  │       ├── 2025-06-15.zip     # all snapshots from June 15
  │       ├── 2025-06-16.zip
  │       └── ...
  └── videos/
      └── {camera_id}/
          ├── 2025-06-15.zip     # all videos from June 15
          └── ...
```

Snapshot zips contain flat JPG files named by capture time: `{HHMMSS}.jpg`.
Video zips contain MP4 files named: `timelapse_{camera_id}_{date}_h{hour}.mp4`.

## Dashboard archive access

When a snapshot has `archive_path` set:

1. Backend serves image at `/api/snapshots/{id}/image` (new endpoint)
2. Reads `archive_path = "snapshots/3/2025-06-15.zip::150322.jpg"`
3. Opens `data/archives/snapshots/3/2025-06-15.zip`
4. Extracts `150322.jpg` from the zip
5. Returns `Response(content=..., media_type="image/jpeg")`
6. Response cached via `Cache-Control: public, max-age=3600`

Videos in archive: `/api/videos/download/{filename}` checks archive if file not in raw dir.

## Migration

Since DB may already have snapshots older than 7 days, the first retention run will:
- Zip everything older than 7 days (in batches)
- Delete everything older than 30 days
- Backfill `archive_path` for existing rows

Add `_migrate_archive_column()` to `database.py` similar to `_migrate_interval_column()`.

## Edge cases

| Scenario | Handling |
|---|---|
| No snapshots to zip | `zip_old_snapshots` returns 0, logs debug |
| Zip already exists | Open and append, or skip if daily zip complete |
| Archive file deleted manually | `archive_path` points to missing zip → return 404 with logged warning |
| Retention = 0 | Disables cleanup (skip if <= 0) |
| Snapshot removed from zip during retention but other snapshots still reference zip | Only delete zip when all its snapshots are expired |
| ffmpeg/video file in use | `shutil.rmtree` with `ignore_errors=True` as done elsewhere |

## Order of implementation

1. Config (Settings class)
2. Model + schema migration
3. RetentionService
4. Snapshot serving from archive
5. Scheduler job
6. Lifespan wiring
7. Env examples
