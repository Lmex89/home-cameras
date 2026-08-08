import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Link } from 'react-router-dom';
import { api, imgPath } from '../api/client';
import type { CameraWithLastSnapshot, DetectionRow, PendingReviewItem } from '../api/types';
import { AmbientBackground } from '../components/Ambient';
import { Lightbox } from '../components/Lightbox';
import type { LightboxItem } from '../components/Lightbox';
import { MobileReviews } from '../components/MobileReviews';
import type { MobileDetectionRow, MobileReviewStat } from '../components/MobileReviews';
import { useToast } from '../components/Toast';
import { useMediaQuery } from '../hooks/useMediaQuery';
import { clsColor, parseDetections, reasonClass, reasonLabel, tagClass } from '../utils/detections';
import { formatTime, formatTimeShort, todayStr } from '../utils/format';

const DETECTIONS_PAGE_SIZE = 30;

type Tab = 'reviews' | 'detections';

interface DetectionItem {
  analysis: DetectionRow;
  snap: { image_path: string; captured_at: string };
  camName: string;
  camera_id: number;
}

const CLASS_LEGEND = [
  ['person', '#ff9f0a'],
  ['car', '#0a84ff'],
  ['train', '#ff453a'],
  ['dog', '#bb88ff'],
  ['bicycle', '#66ddff'],
  ['bird', '#88dd88'],
  ['bench', '#ddaa66'],
  ['motorcycle', '#ff88aa'],
  ['skateboard', '#ffaa66'],
  ['parking meter', '#66aaff'],
] as const;

export default function Reviews() {
  const { toast } = useToast();
  const isMobile = !useMediaQuery('(min-width: 768px)');
  const [tab, setTab] = useState<Tab>('reviews');
  const [cameras, setCameras] = useState<CameraWithLastSnapshot[]>([]);
  const [filterCamera, setFilterCamera] = useState('all');
  const [filterReason, setFilterReason] = useState('all');
  const [filterClass, setFilterClass] = useState('all');
  const [filterDate, setFilterDate] = useState(todayStr());

  const [allReviews, setAllReviews] = useState<PendingReviewItem[]>([]);
  const [selectedIds, setSelectedIds] = useState<Set<number>>(new Set());

  const [allDetections, setAllDetections] = useState<DetectionItem[]>([]);
  const [activeDates, setActiveDates] = useState<Set<string>>(new Set());
  const [hasMore, setHasMore] = useState(false);
  const [detectionsLoading, setDetectionsLoading] = useState(false);
  const [loadMoreLoading, setLoadMoreLoading] = useState(false);
  const detectionsPageRef = useRef(0);

  const [stats, setStats] = useState({ pending: 0, person: 0, unexp: 0, cameras: 0, today: 0 });
  const [summary, setSummary] = useState<[string, number][]>([]);
  const [lb, setLb] = useState<{ items: LightboxItem[]; index: number } | null>(null);

  const filteredReviews = useMemo(() => {
    return allReviews.filter((r) => {
      if (filterCamera !== 'all' && r.camera_id !== parseInt(filterCamera, 10)) return false;
      if (filterReason !== 'all') {
        const rsn = (r.review_reason || '').toLowerCase();
        if (filterReason === 'person_after_hours' && !rsn.includes('person_after_hours')) return false;
        if (filterReason === 'high_person_count' && !rsn.includes('high_person_count')) return false;
        if (filterReason === 'unexpected_objects' && !rsn.includes('unexpected_objects')) return false;
      }
      return true;
    });
  }, [allReviews, filterCamera, filterReason]);

  const filteredDetections = useMemo(() => {
    let items = allDetections;
    if (filterCamera !== 'all') items = items.filter((d) => d.camera_id === parseInt(filterCamera, 10));
    if (filterClass !== 'all') {
      items = items.filter((d) =>
        parseDetections(d.analysis.objects_json).some((o) => o.class_name === filterClass),
      );
    }
    if (activeDates.size > 0) {
      items = items.filter((d) => activeDates.has(d.snap.captured_at.split('T')[0]));
    }
    return items;
  }, [allDetections, filterCamera, filterClass, activeDates]);

  const loadCameras = useCallback(async () => {
    try {
      const data = await api.manifest();
      setCameras(data.cameras || []);
    } catch {
      /* filter stays empty */
    }
  }, []);

  const loadReviews = useCallback(async () => {
    try {
      const [reviews, countRes] = await Promise.all([api.pendingReviews(), api.reviewCount()]);
      setAllReviews(reviews);
      setStats((prev) => ({ ...prev, pending: countRes.count ?? reviews.length }));
    } catch {
      /* list stays */
    }
  }, []);

  const loadStats = useCallback(async () => {
    try {
      const items = await api.detections({ date_from: todayStr(), limit: 500 });
      let totalDetected = 0;
      const clsCounts: Record<string, number> = {};
      for (const d of items) {
        const objs = parseDetections(d.objects_json);
        totalDetected += objs.length;
        for (const o of objs) clsCounts[o.class_name] = (clsCounts[o.class_name] || 0) + 1;
      }
      const cams = new Set(items.map((d) => d.camera_id)).size;
      setStats((prev) => ({ ...prev, today: totalDetected, cameras: cams }));
      setSummary(Object.entries(clsCounts).sort((a, b) => b[1] - a[1]));
    } catch {
      /* stats stay */
    }
  }, []);

  const loadDetections = useCallback(
    async (reset: boolean) => {
      if (reset) {
        detectionsPageRef.current = 0;
        setAllDetections([]);
        setActiveDates(new Set());
        setHasMore(false);
        setDetectionsLoading(true);
      } else {
        setLoadMoreLoading(true);
      }
      const camFilter = filterCamera !== 'all' ? parseInt(filterCamera, 10) : undefined;
      try {
        const items = await api.detections({
          date_from: filterDate,
          camera_id: camFilter,
          limit: DETECTIONS_PAGE_SIZE,
          offset: detectionsPageRef.current * DETECTIONS_PAGE_SIZE,
        });
        if (items.length > 0) {
          const mapped: DetectionItem[] = items.map((d) => ({
            analysis: d,
            snap: { image_path: d.image_path, captured_at: d.captured_at },
            camName: d.camera_name,
            camera_id: d.camera_id,
          }));
          setAllDetections((prev) => [...prev, ...mapped]);
          setActiveDates((prev) => {
            const next = new Set(prev);
            for (const d of mapped) {
              const dt = d.snap.captured_at.split('T')[0];
              if (dt) next.add(dt);
            }
            return next;
          });
          detectionsPageRef.current += 1;
        }
        setHasMore(items.length >= DETECTIONS_PAGE_SIZE);
      } catch {
        setAllDetections([]);
      } finally {
        setDetectionsLoading(false);
        setLoadMoreLoading(false);
      }
    },
    [filterCamera, filterDate],
  );

  useEffect(() => {
    loadCameras();
    loadReviews();
    loadStats();
    const reviewsTimer = window.setInterval(loadReviews, 15000);
    const statsTimer = window.setInterval(loadStats, 60000);
    return () => {
      window.clearInterval(reviewsTimer);
      window.clearInterval(statsTimer);
    };
  }, [loadCameras, loadReviews, loadStats]);

  useEffect(() => {
    const person = filteredReviews.filter((r) => (r.review_reason || '').includes('person_after_hours')).length;
    const unexp = filteredReviews.filter((r) => (r.review_reason || '').includes('unexpected_objects')).length;
    setStats((prev) => ({ ...prev, person, unexp }));
  }, [filteredReviews]);

  const markReviewed = useCallback(
    async (analysisId: number, reviewRequired: boolean, reason?: string) => {
      try {
        await api.updateReview(analysisId, { review_required: reviewRequired, review_reason: reason ?? null });
        toast('Marked as reviewed');
        await loadReviews();
      } catch (err) {
        toast(`Failed: ${err instanceof Error ? err.message : String(err)}`, 'error');
      }
    },
    [loadReviews, toast],
  );

  const bulkAction = useCallback(
    async (reviewRequired: boolean, reason?: string) => {
      const ids = [...selectedIds];
      if (ids.length === 0) return;
      if (!window.confirm(`${reviewRequired ? 'Re-flag' : 'Mark as reviewed'} ${ids.length} items?`)) return;
      try {
        const result = await api.bulkReview({
          analysis_ids: ids,
          review_required: reviewRequired,
          review_reason: reason ?? null,
        });
        toast(`${result.updated} items updated`);
        setSelectedIds(new Set());
        await loadReviews();
      } catch (err) {
        toast(`Bulk action failed: ${err instanceof Error ? err.message : String(err)}`, 'error');
      }
    },
    [selectedIds, loadReviews, toast],
  );

  const toggleDatePill = useCallback((date: string) => {
    setActiveDates((prev) => {
      const next = new Set(prev);
      if (next.has(date)) next.delete(date);
      else next.add(date);
      return next;
    });
  }, []);

  const allDates = useMemo(() => {
    const dates = new Set<string>();
    for (const d of allDetections) {
      const dt = d.snap.captured_at.split('T')[0];
      if (dt) dates.add(dt);
    }
    return [...dates].sort().reverse();
  }, [allDetections]);

  const classCounts = useMemo(() => {
    const counts: Record<string, number> = {};
    for (const d of allDetections) {
      for (const o of parseDetections(d.analysis.objects_json)) {
        counts[o.class_name] = (counts[o.class_name] || 0) + 1;
      }
    }
    return counts;
  }, [allDetections]);

  const summaryBar = useMemo(() => {
    const entries = Object.entries(classCounts).sort((a, b) => b[1] - a[1]);
    const totalObjects = entries.reduce((s, [, n]) => s + n, 0);
    return { entries, totalObjects };
  }, [classCounts]);

  const openReviewLightbox = useCallback((index: number) => {
    setLb({
      items: filteredReviews.map((r) => ({
        image_path: r.image_path,
        label: `${r.camera_name || 'Camera ' + r.camera_id} — ${formatTime(r.captured_at)}`,
      })),
      index,
    });
  }, [filteredReviews]);

  const openDetectionLightbox = useCallback((index: number) => {
    setLb({
      items: filteredDetections.map((d) => ({
        image_path: d.snap.image_path,
        label: `${d.camName} — ${formatTimeShort(d.snap.captured_at)}`,
      })),
      index,
    });
  }, [filteredDetections]);

  const toggleSelectAll = useCallback((checked: boolean) => {
    setSelectedIds(checked ? new Set(filteredReviews.map((r) => r.analysis_id)) : new Set());
  }, [filteredReviews]);

  const toggleSelect = useCallback((id: number) => {
    setSelectedIds((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }, []);

  const switchTab = useCallback(
    (next: Tab) => {
      setTab(next);
      if (next === 'detections') loadDetections(true);
    },
    [loadDetections],
  );

  const handleDateChange = useCallback(
    (date: string) => {
      setFilterDate(date);
      loadDetections(true);
    },
    [loadDetections],
  );

  const mobileStats: MobileReviewStat[] = [
    { id: 'pending', label: 'Pending', value: stats.pending, tone: 'danger' },
    { id: 'person', label: 'Person', value: stats.person, tone: 'warn' },
    { id: 'unexp', label: 'Unexpected', value: stats.unexp, tone: 'accent' },
    { id: 'today', label: 'Today', value: stats.today, tone: 'ok' },
  ];

  const mobileDetections: MobileDetectionRow[] = filteredDetections.map((d) => ({
    analysis_id: d.analysis.analysis_id,
    camera_name: d.camName,
    captured_at: d.snap.captured_at,
    image_path: d.snap.image_path,
    review_required: d.analysis.review_required,
    review_reason: d.analysis.review_reason,
    objects_json: d.analysis.objects_json,
  }));

  if (isMobile) {
    return (
      <MobileReviews
        siteName="SecureView"
        pending={filteredReviews}
        detections={mobileDetections}
        tab={tab}
        stats={mobileStats}
        filterDate={filterDate}
        detectionsLoading={detectionsLoading}
        hasMore={hasMore}
        loadMoreLoading={loadMoreLoading}
        onTabChange={switchTab}
        onDateChange={handleDateChange}
        onLoadMore={() => loadDetections(false)}
        onRefresh={() => {
          loadReviews();
          loadStats();
        }}
        onApprove={(id) => markReviewed(id, false)}
        onDismiss={(id) => markReviewed(id, false, 'false_positive')}
      />
    );
  }

  return (
    <div className="relative min-h-screen overflow-hidden bg-canvas font-sans text-ink">
      <AmbientBackground />

      <header className="relative z-50 border-b border-edge-soft bg-glass/70 backdrop-blur-md">
        <div className="flex items-center justify-between px-6 py-4">
          <div className="flex items-center gap-6">
            <div className="flex items-center gap-3">
              <span className="flex h-9 w-9 items-center justify-center rounded-lg bg-accent-soft text-accent-strong shadow-[0_0_14px_rgba(10,132,255,0.25)]">
                <span className="material-symbols-outlined text-[18px]">shield_lock</span>
              </span>
              <div className="leading-tight">
                <div className="text-sm font-bold tracking-tight text-ink">SecureView</div>
                <div className="font-mono text-[10px] font-bold uppercase tracking-[0.2em] text-accent">
                  // review console
                </div>
              </div>
            </div>
            <nav className="flex gap-2">
              <Link
                to="/"
                className="rounded-lg border border-edge px-3.5 py-2 font-mono text-[10px] font-bold uppercase tracking-[0.16em] text-ink-dim transition-all hover:border-accent hover:text-accent-strong active:scale-95"
              >
                Dashboard
              </Link>
              <span className="rounded-lg border border-accent bg-accent px-3.5 py-2 font-mono text-[10px] font-bold uppercase tracking-[0.16em] text-canvas shadow-[0_0_14px_rgba(10,132,255,0.3)]">
                Review
              </span>
            </nav>
          </div>
          <div className="flex items-center gap-2">
            <button
              onClick={() => {
                loadReviews();
                loadStats();
              }}
              className="flex min-h-11 items-center gap-2 rounded-lg border border-edge px-3.5 font-mono text-[10px] font-bold uppercase tracking-[0.16em] text-ink-dim transition-all hover:border-accent hover:text-accent-strong active:scale-95"
            >
              <span className="material-symbols-outlined text-[16px]">refresh</span>
              Sync
            </button>
          </div>
        </div>

        <div className="flex gap-3 px-6 pb-4">
          <HeaderStat label="Pending" value={stats.pending} tone="danger" />
          <HeaderStat label="Person Alerts" value={stats.person} tone="warn" />
          <HeaderStat label="Unexpected" value={stats.unexp} tone="accent" />
          <HeaderStat label="Cameras" value={stats.cameras} tone="ok" />
          <HeaderStat label="Detected Today" value={stats.today} tone="dim" />
        </div>
      </header>

      <div className="relative z-10 grid min-h-0 grid-cols-[280px_1fr]">
        <aside className="h-[calc(100vh-140px)] overflow-y-auto border-r border-edge-soft bg-glass/40 p-5 backdrop-blur-md">
          <h3 className="mb-4 font-mono text-[10px] font-bold uppercase tracking-[0.24em] text-ink-faint">
            Filters
          </h3>

          <FilterGroup label="Camera">
            <select
              value={filterCamera}
              onChange={(e) => {
                setFilterCamera(e.target.value);
                if (tab === 'detections') loadDetections(true);
              }}
              className="filter-input"
            >
              <option value="all">All Cameras</option>
              {cameras.map((cam) => (
                <option key={cam.id} value={cam.id}>
                  {cam.name || `Camera ${cam.id}`}
                </option>
              ))}
            </select>
          </FilterGroup>

          <FilterGroup label="Reason" visible={tab === 'reviews'}>
            <select
              value={filterReason}
              onChange={(e) => setFilterReason(e.target.value)}
              className="filter-input"
            >
              <option value="all">All Reasons</option>
              <option value="person_after_hours">Person After Hours</option>
              <option value="high_person_count">High Person Count</option>
              <option value="unexpected_objects">Unexpected Objects</option>
            </select>
          </FilterGroup>

          <FilterGroup label="Object Class" visible={tab === 'detections'}>
            <select
              value={filterClass}
              onChange={(e) => setFilterClass(e.target.value)}
              className="filter-input"
            >
              <option value="all">All Classes</option>
              {Object.keys(classCounts)
                .sort()
                .map((cls) => (
                  <option key={cls} value={cls}>
                    {cls}
                  </option>
                ))}
            </select>
          </FilterGroup>

          <FilterGroup label="Detection Class Legend">
            <div className="flex flex-wrap gap-x-3 gap-y-1.5">
              {CLASS_LEGEND.map(([cls, color]) => (
                <span key={cls} className="flex items-center gap-1.5 font-mono text-[9px] uppercase tracking-[0.1em] text-ink-faint">
                  <span className="h-2 w-2 rounded-full" style={{ background: color }}></span>
                  {cls}
                </span>
              ))}
            </div>
          </FilterGroup>

          <div className="mt-5 border-t border-edge-soft pt-4">
            <h3 className="mb-3 font-mono text-[10px] font-bold uppercase tracking-[0.24em] text-ink-faint">
              Today's Summary
            </h3>
            {summary.length ? (
              <div className="flex flex-col gap-1.5">
                {summary.slice(0, 6).map(([cls, n]) => (
                  <div key={cls} className="flex items-center justify-between font-mono text-[11px] text-ink-dim">
                    <span className="flex items-center gap-2">
                      <span className="h-1.5 w-1.5 rounded-full" style={{ background: clsColor(cls) }}></span>
                      {cls}
                    </span>
                    <span className="tabular-nums text-ink">{n}</span>
                  </div>
                ))}
              </div>
            ) : (
              <p className="font-mono text-[10px] text-ink-faint">No detections today</p>
            )}
          </div>
        </aside>

        <main className="min-w-0 overflow-y-auto p-5">
          <div className="mb-5 flex gap-2 rounded-xl border border-edge bg-glass p-1 backdrop-blur-md">
            <TabButton active={tab === 'reviews'} label="Pending" count={stats.pending} onClick={() => switchTab('reviews')} />
            <TabButton active={tab === 'detections'} label="Detections" count={filteredDetections.length} onClick={() => switchTab('detections')} />
          </div>

          {tab === 'reviews' ? (
            <div className="animate-feed-in">
              <div className="mb-4 flex items-center justify-between">
                <h2 className="text-lg font-bold tracking-tight text-ink">
                  Pending Reviews{' '}
                  <span className="ml-2 rounded-full bg-danger-soft px-2 py-0.5 font-mono text-[10px] font-bold tabular-nums text-danger">
                    {stats.pending}
                  </span>
                </h2>
                {selectedIds.size > 0 && (
                  <div className="flex items-center gap-2">
                    <span className="font-mono text-[10px] uppercase tracking-[0.14em] text-ink-dim">
                      {selectedIds.size} selected
                    </span>
                    <button
                      onClick={() => bulkAction(false)}
                      className="rounded-lg border border-ok/50 bg-ok-soft px-3 py-1.5 font-mono text-[10px] font-bold uppercase tracking-[0.14em] text-ok transition-all hover:bg-ok hover:text-canvas active:scale-95"
                    >
                      ✓ Approve All
                    </button>
                    <button
                      onClick={() => bulkAction(false, 'false_positive')}
                      className="rounded-lg border border-danger/40 bg-danger-soft px-3 py-1.5 font-mono text-[10px] font-bold uppercase tracking-[0.14em] text-danger transition-all hover:bg-danger hover:text-white active:scale-95"
                    >
                      ✕ Dismiss All
                    </button>
                    <button
                      onClick={() => setSelectedIds(new Set())}
                      className="rounded-lg border border-edge px-3 py-1.5 font-mono text-[10px] font-bold uppercase tracking-[0.14em] text-ink-dim transition-all hover:border-accent hover:text-accent-strong active:scale-95"
                    >
                      Clear
                    </button>
                  </div>
                )}
              </div>

              {filteredReviews.length === 0 ? (
                <EmptyState icon="task_alt" message="No pending reviews matching filters" />
              ) : (
                <>
                  <label className="mb-4 flex w-fit cursor-pointer items-center gap-2.5">
                    <input
                      type="checkbox"
                      checked={selectedIds.size === filteredReviews.length}
                      onChange={(e) => toggleSelectAll(e.target.checked)}
                      className="accent-accent h-5 w-5 cursor-pointer"
                    />
                    <span className="font-mono text-[10px] font-bold uppercase tracking-[0.18em] text-ink-dim">
                      Select All
                    </span>
                  </label>
                  <div className="flex flex-col gap-3">
                    {filteredReviews.map((r, i) => (
                      <DesktopReviewCard
                        key={r.analysis_id}
                        item={r}
                        index={i}
                        selected={selectedIds.has(r.analysis_id)}
                        onSelect={() => toggleSelect(r.analysis_id)}
                        onApprove={() => markReviewed(r.analysis_id, false)}
                        onDismiss={() => markReviewed(r.analysis_id, false, 'false_positive')}
                        onView={() => openReviewLightbox(i)}
                      />
                    ))}
                  </div>
                </>
              )}
            </div>
          ) : (
            <div className="animate-feed-in" style={{ animationDelay: '60ms' }}>
              <h2 className="mb-4 text-lg font-bold tracking-tight text-ink">All Detected Objects</h2>

              <label className="mb-4 flex w-fit items-center gap-3 rounded-xl border border-edge bg-surface-high px-4 py-2.5">
                <span className="font-mono text-[10px] font-bold uppercase tracking-[0.2em] text-ink-faint">Date</span>
                <input
                  type="date"
                  value={filterDate}
                  max={todayStr()}
                  onChange={(e) => handleDateChange(e.target.value)}
                  className="bg-transparent font-sans text-sm text-ink outline-none [color-scheme:dark]"
                />
              </label>

              <div className="mb-4 flex flex-wrap gap-2">
                <SummaryChip label="snapshots" value={filteredDetections.length} />
                <SummaryChip label="objects" value={summaryBar.totalObjects} />
                {summaryBar.entries.slice(0, 6).map(([cls, n]) => (
                  <span key={cls} className="flex items-center gap-2 rounded-lg border border-edge bg-surface-2/60 px-2.5 py-1.5 font-mono text-[10px] text-ink-dim">
                    <span className="h-1.5 w-1.5 rounded-full" style={{ background: clsColor(cls) }}></span>
                    {cls} <span className="font-bold tabular-nums text-ink">{n}</span>
                  </span>
                ))}
                {summaryBar.entries.length > 6 && (
                  <span className="rounded-lg border border-edge bg-surface-2/60 px-2.5 py-1.5 font-mono text-[10px] text-ink-dim">
                    +{summaryBar.entries.length - 6} more
                  </span>
                )}
              </div>

              {allDates.length > 0 && (
                <div className="mb-4 flex flex-wrap gap-2">
                  {allDates.map((d) => (
                    <button
                      key={d}
                      onClick={() => toggleDatePill(d)}
                      className={`rounded-full px-3.5 py-1.5 font-mono text-[10px] font-bold uppercase tracking-[0.1em] transition-all active:scale-95 ${
                        activeDates.has(d)
                          ? 'bg-accent text-canvas shadow-[0_0_12px_rgba(10,132,255,0.3)]'
                          : 'border border-edge text-ink-dim hover:border-accent hover:text-accent-strong'
                      }`}
                    >
                      {d}
                    </button>
                  ))}
                </div>
              )}

              {Object.keys(classCounts).length > 0 && (
                <div className="mb-4 flex flex-wrap gap-2">
                  {summaryBar.entries.map(([cls, n]) => (
                    <button
                      key={cls}
                      onClick={() => setFilterClass((prev) => (prev === cls ? 'all' : cls))}
                      className={`rounded-full px-3 py-1.5 font-mono text-[10px] font-bold uppercase tracking-[0.08em] transition-all active:scale-95 ${
                        filterClass === cls
                          ? 'bg-warn text-canvas shadow-[0_0_12px_rgba(245,158,11,0.3)]'
                          : 'border border-edge text-ink-dim hover:border-warn hover:text-warn'
                      }`}
                    >
                      {cls} <span className="opacity-60 tabular-nums">{n}</span>
                    </button>
                  ))}
                </div>
              )}

              {detectionsLoading ? (
                <div className="flex items-center justify-center py-20">
                  <div className="h-7 w-7 animate-spin rounded-full border-2 border-edge border-t-accent" />
                </div>
              ) : filteredDetections.length === 0 ? (
                <EmptyState icon="search_off" message="No detections match filters" />
              ) : (
                <>
                  <div className="grid grid-cols-2 gap-3 xl:grid-cols-3 2xl:grid-cols-4">
                    {filteredDetections.map((d, i) => (
                      <DesktopDetectionCard key={d.analysis.analysis_id} item={d} index={i} onView={() => openDetectionLightbox(i)} />
                    ))}
                  </div>
                  {hasMore && (
                    <div className="flex justify-center pt-5">
                      <button
                        onClick={() => loadDetections(false)}
                        disabled={loadMoreLoading}
                        className="flex min-h-11 items-center gap-2 rounded-lg border border-edge px-6 font-mono text-[10px] font-bold uppercase tracking-[0.18em] text-ink-dim transition-all hover:border-accent hover:text-accent-strong active:scale-95 disabled:opacity-50"
                      >
                        {loadMoreLoading ? (
                          <>
                            <span className="h-3 w-3 animate-spin rounded-full border border-edge border-t-accent" />
                            Loading
                          </>
                        ) : (
                          '⟳ Load More'
                        )}
                      </button>
                    </div>
                  )}
                </>
              )}
            </div>
          )}
        </main>
      </div>

      {lb && (
        <Lightbox
          items={lb.items}
          index={lb.index}
          onClose={() => setLb(null)}
          onIndexChange={(index) => setLb((prev) => (prev ? { ...prev, index } : prev))}
        />
      )}
    </div>
  );
}

const HEADER_STAT_TONES: Record<string, string> = {
  accent: 'text-accent-strong',
  ok: 'text-ok',
  warn: 'text-warn',
  danger: 'text-danger',
  dim: 'text-ink-faint',
};

function HeaderStat({
  label,
  value,
  tone,
}: {
  label: string;
  value: number;
  tone: 'accent' | 'ok' | 'warn' | 'danger' | 'dim';
}) {
  return (
    <div className="flex min-w-[110px] flex-col rounded-xl border border-edge bg-surface-high px-4 py-2.5">
      <span className="text-[0.55rem] uppercase tracking-[0.22em] text-ink-faint">{label}</span>
      <span className={`text-xl font-bold tabular-nums ${HEADER_STAT_TONES[tone]}`}>{value}</span>
    </div>
  );
}

function TabButton({
  active,
  label,
  count,
  onClick,
}: {
  active: boolean;
  label: string;
  count: number;
  onClick: () => void;
}) {
  return (
    <button
      onClick={onClick}
      className={`flex min-h-11 flex-1 items-center justify-center gap-2 rounded-lg font-mono text-[10px] font-bold uppercase tracking-[0.16em] transition-all active:scale-95 ${
        active ? 'bg-accent text-canvas shadow-[0_0_14px_rgba(10,132,255,0.3)]' : 'text-ink-dim hover:text-ink'
      }`}
    >
      {label}
      <span className={`rounded-full px-1.5 py-0.5 text-[9px] tabular-nums ${active ? 'bg-canvas/20' : 'bg-surface-2'}`}>
        {count}
      </span>
    </button>
  );
}

function FilterGroup({
  label,
  children,
  visible = true,
}: {
  label: string;
  children: React.ReactNode;
  visible?: boolean;
}) {
  if (!visible) return null;
  return (
    <div className="mb-5">
      <label className="mb-2 block font-mono text-[9px] font-bold uppercase tracking-[0.22em] text-ink-faint">
        {label}
      </label>
      {children}
    </div>
  );
}

function DesktopReviewCard({
  item,
  index,
  selected,
  onSelect,
  onApprove,
  onDismiss,
  onView,
}: {
  item: PendingReviewItem;
  index: number;
  selected: boolean;
  onSelect: () => void;
  onApprove: () => void;
  onDismiss: () => void;
  onView: () => void;
}) {
  const detections = parseDetections(item.objects_json);
  const tags = detections.map((d) => (
    <span key={d.class_name} className={`rounded-md border px-1.5 py-0.5 text-[10px] ${tagClass(d.class_name)}`}>
      {d.class_name} <span className="tabular-nums opacity-70">{(d.confidence * 100).toFixed(0)}%</span>
    </span>
  ));

  return (
    <div
      className="animate-feed-in grid gap-3 rounded-xl border border-edge bg-glass p-3 backdrop-blur-md transition-all hover:border-accent/40 xl:grid-cols-[28px_200px_1fr_auto]"
      style={{ animationDelay: `${index * 60}ms` }}
    >
      <input
        type="checkbox"
        checked={selected}
        onChange={onSelect}
        className="accent-accent h-5 w-5 cursor-pointer self-center"
      />
      <img
        src={imgPath(item.image_path)}
        alt=""
        onClick={onView}
        onError={(e) => ((e.target as HTMLImageElement).style.display = 'none')}
        className="h-[110px] w-full cursor-zoom-in rounded-lg object-cover xl:h-[120px] xl:w-[200px]"
      />
      <div className="flex min-w-0 flex-col gap-1.5">
        <div className="flex items-center gap-2">
          <span className="text-sm font-bold tracking-tight text-ink">
            {item.camera_name || `Camera ${item.camera_id}`}
          </span>
          <span className={`rounded-md px-1.5 py-0.5 text-[9px] font-bold uppercase tracking-[0.12em] ${reasonClass(item.review_reason) || 'bg-surface-2 text-ink-dim'}`}>
            {reasonLabel(item.review_reason)}
          </span>
        </div>
        <span className="font-mono text-[10px] tabular-nums text-ink-faint">
          {formatTime(item.captured_at)}
        </span>
        {tags.length > 0 ? (
          <div className="flex flex-wrap gap-1.5">{tags}</div>
        ) : (
          <span className="text-[10px] text-ink-faint">No objects</span>
        )}
        {item.person_count > 0 && (
          <span className="font-mono text-[10px] font-bold uppercase tracking-[0.12em] text-warn">
            {item.person_count} person(s)
          </span>
        )}
      </div>
      <div className="flex flex-row gap-2 self-end xl:flex-col xl:self-auto">
        <button
          onClick={onView}
          className="flex h-11 min-w-[84px] items-center justify-center gap-2 rounded-lg border border-edge font-mono text-[10px] font-bold uppercase tracking-[0.14em] text-ink-dim transition-all hover:border-accent hover:text-accent-strong active:scale-95"
        >
          <span className="material-symbols-outlined text-[16px]">zoom_in</span>
          View
        </button>
        <button
          onClick={onApprove}
          className="flex h-11 min-w-[84px] items-center justify-center gap-2 rounded-lg border border-ok/50 bg-ok-soft font-mono text-[10px] font-bold uppercase tracking-[0.14em] text-ok transition-all hover:bg-ok hover:text-canvas active:scale-95"
        >
          <span className="material-symbols-outlined text-[16px]">check</span>
          Approve
        </button>
        <button
          onClick={onDismiss}
          className="flex h-11 min-w-[84px] items-center justify-center gap-2 rounded-lg border border-edge font-mono text-[10px] font-bold uppercase tracking-[0.14em] text-ink-dim transition-all hover:border-danger hover:text-danger active:scale-95"
        >
          <span className="material-symbols-outlined text-[16px]">close</span>
          Dismiss
        </button>
      </div>
    </div>
  );
}

function DesktopDetectionCard({
  item,
  index,
  onView,
}: {
  item: DetectionItem;
  index: number;
  onView: () => void;
}) {
  const objs = parseDetections(item.analysis.objects_json);
  const tags = objs.slice(0, 3).map((o) => (
    <span key={o.class_name} className={`rounded-md border px-1.5 py-0.5 text-[9px] ${tagClass(o.class_name)}`}>
      {o.class_name}
    </span>
  ));

  return (
    <div
      className="animate-feed-in overflow-hidden rounded-xl border border-edge bg-glass backdrop-blur-md transition-all hover:border-accent/40 active:scale-[0.98]"
      style={{ animationDelay: `${index * 45}ms` }}
    >
      <div className="relative h-32 overflow-hidden bg-gradient-to-br from-surface-2 via-surface to-canvas">
        <img
          src={imgPath(item.snap.image_path)}
          alt=""
          className="h-full w-full object-cover"
          onClick={onView}
          onError={(e) => ((e.target as HTMLImageElement).style.display = 'none')}
        />
        {item.analysis.review_required && (
          <span className="absolute right-2 top-2 flex h-6 w-6 items-center justify-center rounded-full bg-danger-soft text-danger shadow-[0_0_10px_rgba(255,69,58,0.3)]">
            <span className="material-symbols-outlined text-[14px]">warning</span>
          </span>
        )}
      </div>
      <div className="px-3 py-2.5">
        <div className="flex items-center justify-between gap-2">
          <span className="truncate text-xs font-bold text-ink">{item.camName}</span>
          <span className="shrink-0 font-mono text-[9px] tabular-nums text-ink-faint">
            {formatTimeShort(item.snap.captured_at)}
          </span>
        </div>
        {tags.length > 0 && <div className="mt-1.5 flex flex-wrap gap-1">{tags}</div>}
      </div>
    </div>
  );
}

function SummaryChip({ label, value }: { label: string; value: number }) {
  return (
    <span className="rounded-lg border border-edge bg-surface-2/60 px-2.5 py-1.5 font-mono text-[10px] text-ink-dim">
      <span className="font-bold tabular-nums text-ink">{value}</span> {label}
    </span>
  );
}

function EmptyState({ icon, message }: { icon: string; message: string }) {
  return (
    <div className="flex flex-col items-center gap-3 rounded-xl border border-edge bg-surface-high py-16 text-center">
      <span className="material-symbols-outlined text-4xl text-ink-faint/60">{icon}</span>
      <p className="font-mono text-[11px] uppercase tracking-[0.24em] text-ink-dim">{message}</p>
    </div>
  );
}
