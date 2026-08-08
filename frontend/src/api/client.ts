import type {
  BulkReviewPayload,
  BulkReviewResult,
  Camera,
  CameraCreatePayload,
  CameraTestResult,
  CameraWithLastSnapshot,
  DetectionRow,
  ForceSnapshotResult,
  Manifest,
  PendingReviewItem,
  ReviewUpdate,
  VideoRequest,
  VideoResponse,
} from './types';

const IMG_BASE = '/snapshots/';

async function request<T>(url: string, init?: RequestInit): Promise<T> {
  const res = await fetch(url, init);
  if (!res.ok) {
    let detail = `HTTP ${res.status}`;
    try {
      const body = await res.json();
      if (Array.isArray(body.detail)) {
        detail = body.detail.map((e: { msg: string }) => e.msg).join('; ');
      } else if (body.detail) {
        detail = body.detail;
      } else if (body.error) {
        detail = body.error;
      }
    } catch {
      /* keep default */
    }
    throw new Error(detail);
  }
  if (res.status === 204) return undefined as T;
  return res.json() as Promise<T>;
}

function postJSON<T>(url: string, body: unknown): Promise<T> {
  return request<T>(url, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
}

export const api = {
  manifest(): Promise<Manifest> {
    return request<Manifest>(`/api/data/manifest.json?_=${Date.now()}`);
  },

  dashboard(): Promise<CameraWithLastSnapshot[]> {
    return request<CameraWithLastSnapshot[]>('/api/cameras');
  },

  camera(id: number): Promise<Camera> {
    return request<Camera>(`/api/cameras/${id}`);
  },

  createCamera(payload: CameraCreatePayload): Promise<Camera> {
    return postJSON<Camera>('/api/cameras', payload);
  },

  updateCamera(id: number, payload: CameraCreatePayload): Promise<Camera> {
    return request<Camera>(`/api/cameras/${id}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    });
  },

  deleteCamera(id: number): Promise<void> {
    return request<void>(`/api/cameras/${id}`, { method: 'DELETE' });
  },

  testCamera(payload: CameraCreatePayload): Promise<CameraTestResult> {
    return postJSON<CameraTestResult>('/api/cameras/test', payload);
  },

  forceSnapshot(cameraId: number): Promise<ForceSnapshotResult> {
    return postJSON<ForceSnapshotResult>(`/api/cameras/${cameraId}/snapshot`, {});
  },

  pendingReviews(): Promise<PendingReviewItem[]> {
    return request<PendingReviewItem[]>('/api/reviews/pending');
  },

  reviewCount(): Promise<{ count: number }> {
    return request<{ count: number }>('/api/reviews/count');
  },

  detections(params: {
    date_from?: string;
    camera_id?: number;
    limit?: number;
    offset?: number;
    days_back?: number;
  }): Promise<DetectionRow[]> {
    const q = new URLSearchParams();
    if (params.date_from) q.set('date_from', params.date_from);
    if (params.camera_id) q.set('camera_id', String(params.camera_id));
    if (params.limit !== undefined) q.set('limit', String(params.limit));
    if (params.offset !== undefined) q.set('offset', String(params.offset));
    if (params.days_back !== undefined) q.set('days_back', String(params.days_back));
    return request<DetectionRow[]>(`/api/reviews/detections?${q.toString()}`);
  },

  updateReview(analysisId: number, payload: ReviewUpdate): Promise<unknown> {
    return postJSON<unknown>(`/api/reviews/${analysisId}/review`, payload);
  },

  bulkReview(payload: BulkReviewPayload): Promise<BulkReviewResult> {
    return postJSON<BulkReviewResult>('/api/reviews/bulk-review', payload);
  },

  createVideo(payload: VideoRequest): Promise<VideoResponse> {
    return postJSON<VideoResponse>('/api/videos', payload);
  },
};

export function imgPath(imagePath: string): string {
  return IMG_BASE + imagePath;
}
