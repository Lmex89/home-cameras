export interface Camera {
  id: number;
  name: string;
  host: string;
  port: number;
  username: string;
  password: string;
  profile_token: string | null;
  snapshot_url: string | null;
  interval_seconds: number;
  enabled: boolean;
  created_at: string;
  updated_at: string;
}

export interface Snapshot {
  id: number;
  camera_id: number;
  captured_at: string;
  image_path: string;
  file_size: number;
  status: string;
  error_message: string | null;
  archive_path: string | null;
}

export interface CameraWithLastSnapshot extends Camera {
  last_snapshot: Snapshot | null;
  total_snapshots: number;
}

export interface ManifestSnapshot {
  image_path: string;
  captured_at: string;
  file_size: number;
}

export interface Manifest {
  generated_at: string;
  cameras: CameraWithLastSnapshot[];
  snapshots: Record<string, Record<string, ManifestSnapshot[]>>;
}

export interface DetectedObject {
  class_name: string;
  confidence: number;
  bbox?: number[];
}

export interface PendingReviewItem {
  analysis_id: number;
  snapshot_id: number;
  camera_id: number;
  camera_name: string;
  captured_at: string;
  image_path: string;
  model_name: string;
  person_count: number;
  review_required: boolean;
  review_reason: string | null;
  anomaly_score: number | null;
  error_message: string | null;
  objects_json: string | null;
  analyzed_at: string;
}

export interface DetectionRow {
  analysis_id: number;
  snapshot_id: number;
  camera_id: number;
  camera_name: string;
  captured_at: string;
  image_path: string;
  model_name: string;
  objects_json: string | null;
  person_count: number;
  review_required: boolean;
  review_reason: string | null;
  analyzed_at: string;
}

export interface SnapshotAnalysis {
  id: number;
  snapshot_id: number;
  model_name: string;
  model_version: string;
  status: string;
  objects_json: string | null;
  person_count: number;
  review_required: boolean;
  review_reason: string | null;
  anomaly_score: number | null;
  error_message: string | null;
  analyzed_at: string;
  created_at: string;
  updated_at: string;
}

export interface SnapshotWithAnalysis {
  snapshot: Snapshot;
  analysis: SnapshotAnalysis | null;
}

export interface CameraCreatePayload {
  name: string;
  host: string;
  port: number;
  username: string;
  password: string;
  snapshot_url: string | null;
  interval_seconds: number;
  enabled: boolean;
}

export interface CameraTestResult {
  reachable: boolean;
  profiles: string[];
  error?: string;
}

export interface ForceSnapshotResult {
  camera_id: number;
  camera_name: string;
  success: boolean;
  error: string | null;
  image_path: string | null;
}

export interface VideoRequest {
  camera_id: number;
  date: string;
  hour: number | null;
}

export interface VideoResponse {
  video_url: string;
}

export interface ReviewUpdate {
  review_required: boolean;
  review_reason?: string | null;
}

export interface BulkReviewPayload {
  analysis_ids: number[];
  review_required: boolean;
  review_reason: string | null;
}

export interface BulkReviewResult {
  updated: number;
  errors: { id: number; error: string }[];
}
