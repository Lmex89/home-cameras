import { useCallback, useEffect, useMemo, useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { api, imgPath } from '../api/client';
import type { CameraWithLastSnapshot, Manifest } from '../api/types';
import { AmbientBackground } from '../components/Ambient';
import { Lightbox } from '../components/Lightbox';
import type { LightboxItem } from '../components/Lightbox';
import { LiveStream } from '../components/LiveStream';
import { MobileDashboard } from '../components/MobileDashboard';
import type { MobileCamera } from '../components/MobileDashboard';
import { useMediaQuery } from '../hooks/useMediaQuery';
import { formatTime, formatTimeShort, getHour, intervalLabel, timeAgo } from '../utils/format';

const REF_INTERVAL = 30000;
const HOUR_OPTIONS = Array.from({ length: 24 }, (_, h) => String(h).padStart(2, '0'));

interface SnapView {
  image_path: string;
  captured_at: string;
  file_size: number;
}

interface PageSnaps {
  pages: number;
  start: number;
  items: SnapView[];
}

function cameraDates(cam: CameraWithLastSnapshot, manifest: Manifest): string[] {
  const byCam = manifest.snapshots[String(cam.id)] || {};
  return Object.keys(byCam).sort().reverse();
}

function dateCount(cam: CameraWithLastSnapshot, manifest: Manifest, date: string): number {
  const byCam = manifest.snapshots[String(cam.id)] || {};
  return byCam[date]?.length ?? 0;
}

function snapsForDate(cam: CameraWithLastSnapshot, manifest: Manifest, date: string): SnapView[] {
  const byCam = manifest.snapshots[String(cam.id)] || {};
  return byCam[date] || [];
}

export default function Dashboard() {
  const navigate = useNavigate();
  const isMobile = !useMediaQuery('(min-width: 768px)');
  const [manifest, setManifest] = useState<Manifest | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [reviewCount, setReviewCount] = useState(0);
  const [selectedId, setSelectedId] = useState<number | null>(null);
  const [dateInput, setDateInput] = useState('');
  const [selHour, setSelHour] = useState<string>(() => HOUR_OPTIONS[new Date().getHours()]);
  const [pageSize, setPageSize] = useState(10);
  const [curPage, setCurPage] = useState(1);
  const [videoBusy, setVideoBusy] = useState(false);
  const [streamingCamId, setStreamingCamId] = useState<number | null>(null);
  const [lb, setLb] = useState<{ items: LightboxItem[]; index: number } | null>(null);
  const [now, setNow] = useState(() => new Date());

  useEffect(() => {
    let cancelled = false;
    const load = async () => {
      try {
        const data = await api.manifest();
        if (cancelled) return;
        setManifest(data);
        setLoadError(null);
      } catch (err) {
        if (!cancelled) setLoadError(err instanceof Error ? err.message : String(err));
      }
    };
    load();
    const timer = window.setInterval(load, REF_INTERVAL);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, []);

  useEffect(() => {
    let cancelled = false;
    const load = async () => {
      try {
        const { count } = await api.reviewCount();
        if (!cancelled) setReviewCount(count);
      } catch {
        /* badge stays stale */
      }
    };
    load();
    const timer = window.setInterval(load, REF_INTERVAL);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, []);

  useEffect(() => {
    const timer = window.setInterval(() => setNow(new Date()), 1000);
    return () => window.clearInterval(timer);
  }, []);

  useEffect(() => {
    const mq = window.matchMedia('(min-width: 960px)');
    const update = () => {
      if (selectedId !== null && !mq.matches) document.body.style.overflow = 'hidden';
      else document.body.style.overflow = '';
    };
    update();
    mq.addEventListener('change', update);
    return () => {
      mq.removeEventListener('change', update);
      document.body.style.overflow = '';
    };
  }, [selectedId]);

  const selectedCam = useMemo(() => {
    if (!manifest || selectedId === null) return null;
    return manifest.cameras.find((c) => c.id === selectedId) || null;
  }, [manifest, selectedId]);

  const dates = useMemo(
    () => (selectedCam && manifest ? cameraDates(selectedCam, manifest) : []),
    [selectedCam, manifest],
  );

  const effectiveDate = useMemo(() => {
    if (!dateInput || !dates.includes(dateInput)) return dates[0] || '';
    return dateInput;
  }, [dateInput, dates]);

  useEffect(() => {
    if (!effectiveDate) return;
    const cam = selectedCam;
    if (!cam || !manifest) return;
    const count = dateCount(cam, manifest, effectiveDate);
    const maxPage = Math.max(1, Math.ceil(count / pageSize));
    if (curPage > maxPage) setCurPage(maxPage);
  }, [effectiveDate, selectedCam, manifest, pageSize, curPage]);

  const openCamera = useCallback((camId: number) => {
    setSelectedId(camId);
    setCurPage(1);
    setStreamingCamId(null);
  }, []);

  const closePanel = useCallback(() => {
    setSelectedId(null);
    setDateInput('');
    setStreamingCamId(null);
  }, []);

  const openLightbox = useCallback((items: SnapView[], index: number) => {
    setLb({
      items: items.map((s) => ({ image_path: s.image_path, label: formatTime(s.captured_at) })),
      index,
    });
  }, []);

  const requestVideo = useCallback(
    async (cameraId: number, date: string, hour: string | null) => {
      setVideoBusy(true);
      try {
        const data = await api.createVideo({
          camera_id: cameraId,
          date,
          hour: hour !== null && hour !== '' ? parseInt(hour, 10) : null,
        });
        window.location.href = data.video_url;
      } catch (err) {
        window.alert(`Failed: ${err instanceof Error ? err.message : String(err)}`);
      } finally {
        setVideoBusy(false);
      }
    },
    [],
  );

  const allSnaps: SnapView[] = useMemo(() => {
    if (!selectedCam || !manifest || !effectiveDate) return [];
    let snaps = snapsForDate(selectedCam, manifest, effectiveDate);
    if (selHour) snaps = snaps.filter((s) => getHour(s.captured_at) === selHour);
    return [...snaps].sort((a, b) => a.captured_at.localeCompare(b.captured_at));
  }, [selectedCam, manifest, effectiveDate, selHour]);

  const pageSnaps: PageSnaps = useMemo(() => {
    const size = pageSize > 0 ? pageSize : Infinity;
    const pages = Math.max(1, Math.ceil(allSnaps.length / size));
    const page = Math.min(curPage, pages);
    const start = (page - 1) * size;
    return { pages, start, items: allSnaps.slice(start, start + size) };
  }, [allSnaps, pageSize, curPage]);

  const totalCams = manifest?.cameras.length ?? 0;
  const totalSnaps = manifest ? manifest.cameras.reduce((s, c) => s + c.total_snapshots, 0) : 0;

  const mobileCameras: MobileCamera[] = useMemo(
    () =>
      (manifest?.cameras ?? []).map((c) => ({
        id: String(c.id),
        name: c.name,
        status: c.last_snapshot ? 'live' : 'offline',
        thumbnailUrl: c.last_snapshot
          ? `${imgPath(c.last_snapshot.image_path)}?_=${Date.now()}`
          : undefined,
        lastSeen: c.last_snapshot?.captured_at,
        ptz: true,
      })),
    [manifest],
  );

  const cycleCamera = useCallback(
    (dir: 'left' | 'right') => {
      if (!manifest || manifest.cameras.length === 0) return;
      const idx = manifest.cameras.findIndex((c) => c.id === selectedId);
      const base = idx >= 0 ? idx : 0;
      const next =
        (base + (dir === 'right' ? 1 : -1) + manifest.cameras.length) % manifest.cameras.length;
      openCamera(manifest.cameras[next].id);
    },
    [manifest, selectedId, openCamera],
  );

  const handleSOS = useCallback(() => {
    if (!manifest || manifest.cameras.length === 0) return;
    const cam = manifest.cameras[0];
    const day = cameraDates(cam, manifest)[0];
    if (!day) return;
    const snaps = snapsForDate(cam, manifest, day);
    if (snaps.length > 0) openLightbox(snaps, 0);
  }, [manifest, openLightbox]);

  const panelHandlers = {
    onDateChange: (v: string) => {
      setDateInput(v);
      setCurPage(1);
    },
    onHourChange: (h: string) => {
      setSelHour(h);
      setCurPage(1);
    },
    onPageSizeChange: (n: number) => {
      setPageSize(n);
      setCurPage(1);
    },
    onPageChange: setCurPage,
    onRequestVideo: requestVideo,
    onOpenLightbox: openLightbox,
    onClose: closePanel,
    streamingCamId,
    onToggleStream: (camId: number) => setStreamingCamId(prev => prev === camId ? null : camId),
  };

  if (isMobile) {
    return (
      <>
        <MobileDashboard
          siteName="SecureView"
          cameras={mobileCameras}
          armed={true}
          activeNav="dashboard"
          notifications={reviewCount}
          onCameraSelect={(cam) => openCamera(Number(cam.id))}
          onPtz={(_cam, dir) => cycleCamera(dir)}
          onArmAll={() => {
            if (manifest && manifest.cameras.length > 0) openCamera(manifest.cameras[0].id);
          }}
          onSOS={handleSOS}
          onNavSelect={(item) => {
            if (item.id === 'events') navigate('/reviews');
          }}
          onNotifications={() => {
            if (manifest && manifest.cameras.length > 0)
              openCamera(selectedId ?? manifest.cameras[0].id);
          }}
        />

        <div className={`panel-mask${selectedId !== null ? ' open' : ''}`} onClick={closePanel}></div>

        <CameraDetailPanel
          selectedCam={selectedCam}
          manifest={manifest}
          dates={dates}
          effectiveDate={effectiveDate}
          selHour={selHour}
          pageSize={pageSize}
          curPage={curPage}
          allSnaps={allSnaps}
          pageSnaps={pageSnaps}
          videoBusy={videoBusy}
          {...panelHandlers}
        />

        {lb && (
          <Lightbox
            items={lb.items}
            index={lb.index}
            onClose={() => setLb(null)}
            onIndexChange={(index) => setLb((prev) => (prev ? { ...prev, index } : prev))}
          />
        )}

        {videoBusy && <VideoLoader />}
      </>
    );
  }

  const clock = now.toLocaleTimeString(undefined, { hour12: false });
  const dateStr = now.toLocaleDateString(undefined, { weekday: 'short', month: 'short', day: 'numeric' });

  return (
    <div className="relative flex h-screen overflow-hidden bg-canvas font-sans text-ink">
      <AmbientBackground />

      <main className="relative z-10 flex min-w-0 flex-1 flex-col">
        <header className="border-b border-edge-soft bg-glass/70 backdrop-blur-md">
          <div className="flex flex-wrap items-center justify-between gap-3 px-6 py-4">
            <div>
              <div className="flex items-center gap-2 text-[0.55rem] uppercase tracking-[0.28em] text-accent">
                <span className="inline-block h-1 w-1 rounded-full bg-accent shadow-[0_0_6px_rgba(10,132,255,0.8)]" />
                Security Center
              </div>
              <h1 className="mt-1 flex items-center gap-3 text-xl font-bold tracking-tight text-ink">
                SecureView
                <span className="font-mono text-[10px] font-bold uppercase tracking-[0.2em] text-ink-faint">
                  surveillance grid
                </span>
              </h1>
            </div>

            <div className="flex items-center gap-4">
              <div className="flex gap-2">
                <StatChip label="Cams" value={totalCams} tone="accent" />
                <StatChip label="Captures" value={totalSnaps} tone="ok" />
              </div>
              <div className="hidden text-right lg:block">
                <div className="text-sm font-semibold tabular-nums text-ink">{clock}</div>
                <div className="text-[0.55rem] uppercase tracking-[0.18em] text-ink-faint">{dateStr}</div>
              </div>
              <Link
                to="/reviews"
                className="flex min-h-11 items-center gap-2 rounded-lg border border-edge px-3.5 font-mono text-[10px] font-bold uppercase tracking-[0.16em] text-ink-dim transition-all hover:border-accent hover:text-accent-strong active:scale-95"
              >
                <span className="material-symbols-outlined text-[16px]">event_note</span>
                Review
                <span className="rounded-full bg-danger-soft px-1.5 py-0.5 tabular-nums text-danger">
                  {reviewCount}
                </span>
              </Link>
            </div>
          </div>
        </header>

        <section className="flex min-h-0 flex-1">
          <div className="grid flex-1 grid-cols-2 gap-4 overflow-y-auto p-5 xl:grid-cols-3 2xl:grid-cols-4">
            {loadError ? (
              <GridState icon="cloud_off" title="Connection Error" detail={loadError} />
            ) : !manifest ? (
              <LoadingState label="Acquiring feed…" />
            ) : manifest.cameras.length === 0 ? (
              <GridState icon="videocam_off" title="No Cameras Found" detail="Add a camera to start monitoring" />
            ) : (
              manifest.cameras.map((cam) => (
                <CameraCard key={cam.id} cam={cam} onClick={() => openCamera(cam.id)} />
              ))
            )}
          </div>

          <CameraDetailPanel
            selectedCam={selectedCam}
            manifest={manifest}
            dates={dates}
            effectiveDate={effectiveDate}
            selHour={selHour}
            pageSize={pageSize}
            curPage={curPage}
            allSnaps={allSnaps}
            pageSnaps={pageSnaps}
            videoBusy={videoBusy}
            {...panelHandlers}
          />
        </section>
      </main>

      {lb && (
        <Lightbox
          items={lb.items}
          index={lb.index}
          onClose={() => setLb(null)}
          onIndexChange={(index) => setLb((prev) => (prev ? { ...prev, index } : prev))}
        />
      )}

      {videoBusy && <VideoLoader />}
    </div>
  );
}

const STAT_CHIP_TONES: Record<string, string> = {
  accent: 'text-accent-strong',
  ok: 'text-ok',
  warn: 'text-warn',
  danger: 'text-danger',
  dim: 'text-ink-dim',
};

function StatChip({
  label,
  value,
  tone,
}: {
  label: string;
  value: number;
  tone: 'accent' | 'ok' | 'warn' | 'danger' | 'dim';
}) {
  return (
    <div className="flex flex-col items-end rounded-xl border border-edge bg-surface-high px-3.5 py-2">
      <span className="text-[0.55rem] uppercase tracking-[0.22em] text-ink-faint">{label}</span>
      <span className={`text-lg font-bold tabular-nums ${STAT_CHIP_TONES[tone]}`}>{value}</span>
    </div>
  );
}

function CameraCard({ cam, onClick }: { cam: CameraWithLastSnapshot; onClick: () => void }) {
  const last = cam.last_snapshot;
  const live = Boolean(last);

  return (
    <div
      onClick={onClick}
      className="group cursor-pointer overflow-hidden rounded-xl border border-edge bg-glass backdrop-blur-md transition-all hover:border-accent/50 hover:shadow-[0_0_28px_rgba(10,132,255,0.14)] active:scale-[0.99]"
    >
      <div className="relative aspect-video overflow-hidden bg-gradient-to-br from-surface-2 via-surface to-canvas">
        {last ? (
          <img
            src={`${imgPath(last.image_path)}?_=${Date.now()}`}
            alt=""
            loading="lazy"
            className="h-full w-full object-cover"
          />
        ) : (
          <div className="absolute inset-0 flex items-center justify-center">
            <span className="material-symbols-outlined text-3xl text-ink-faint/50">videocam</span>
          </div>
        )}
        <div className="pointer-events-none absolute inset-0 bg-[linear-gradient(0deg,rgba(142,142,147,0.05)_1px,transparent_1px)] bg-[size:100%_4px] opacity-50" />

        <span className="absolute left-2.5 top-2.5 rounded-md bg-canvas/70 px-1.5 py-0.5 font-mono text-[10px] font-bold tracking-[0.18em] text-ink-faint backdrop-blur-sm">
          CAM {cam.id}
        </span>
        {live ? (
          <span className="absolute right-2.5 top-2.5 flex items-center gap-1.5 rounded-md bg-danger-soft px-1.5 py-0.5 font-mono text-[10px] font-bold uppercase tracking-[0.16em] text-danger shadow-[0_0_10px_rgba(255,69,58,0.25)] backdrop-blur-sm">
            <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-danger" />
            Live
          </span>
        ) : (
          <span className="absolute right-2.5 top-2.5 rounded-md bg-canvas/70 px-1.5 py-0.5 font-mono text-[10px] font-bold uppercase tracking-[0.16em] text-ink-faint backdrop-blur-sm">
            Offline
          </span>
        )}

        <div className="absolute inset-x-0 bottom-0 bg-gradient-to-t from-canvas/95 to-transparent px-3 pb-2 pt-8">
          <div className="truncate text-sm font-bold tracking-tight text-ink">{cam.name}</div>
          <div className="flex items-center gap-2 font-mono text-[10px] tabular-nums text-ink-faint">
            <span>{cam.total_snapshots} caps</span>
            <span className="opacity-40">|</span>
            <span>every {intervalLabel(cam.interval_seconds)}</span>
          </div>
        </div>
      </div>

      <div className="flex items-center justify-between border-t border-edge-soft px-3 py-2.5">
        <span
          className={`rounded-md px-2 py-0.5 font-mono text-[10px] font-bold uppercase tracking-[0.16em] ${
            cam.enabled
              ? 'bg-ok-soft text-ok'
              : 'bg-surface-2 text-ink-faint'
          }`}
        >
          {cam.enabled ? 'Active' : 'Off'}
        </span>
        <span className="font-mono text-[10px] tabular-nums text-ink-faint">
          {last ? `${timeAgo(last.captured_at)} ago` : 'no signal'}
        </span>
      </div>
    </div>
  );
}

function CameraDetailPanel({
  selectedCam,
  manifest,
  dates,
  effectiveDate,
  selHour,
  pageSize,
  curPage,
  allSnaps,
  pageSnaps,
  videoBusy,
  streamingCamId,
  onToggleStream,
  onDateChange,
  onHourChange,
  onPageSizeChange,
  onPageChange,
  onRequestVideo,
  onOpenLightbox,
  onClose,
}: {
  selectedCam: CameraWithLastSnapshot | null;
  manifest: Manifest | null;
  dates: string[];
  effectiveDate: string;
  selHour: string;
  pageSize: number;
  curPage: number;
  allSnaps: SnapView[];
  pageSnaps: PageSnaps;
  videoBusy: boolean;
  streamingCamId: number | null;
  onToggleStream: (camId: number) => void;
  onDateChange: (date: string) => void;
  onHourChange: (hour: string) => void;
  onPageSizeChange: (size: number) => void;
  onPageChange: (page: number) => void;
  onRequestVideo: (cameraId: number, date: string, hour: string | null) => Promise<void>;
  onOpenLightbox: (items: SnapView[], index: number) => void;
  onClose: () => void;
}) {
  const open = selectedCam !== null;

  return (
    <div
      className={`fixed inset-x-0 bottom-0 z-50 flex max-h-[88dvh] flex-col border-t border-edge-soft bg-glass/90 backdrop-blur-md transition-transform duration-300 md:static md:z-auto md:max-h-none md:w-[420px] md:min-w-[420px] md:translate-y-0 md:border-l md:border-t-0 ${
        open ? 'translate-y-0' : 'translate-y-full'
      }`}
    >
      <div className="mx-auto mt-2 h-1 w-8 shrink-0 rounded-full bg-edge md:hidden"></div>

      <div className="flex shrink-0 items-center justify-between px-5 pb-1 pt-2.5">
        <h2 className="flex items-center gap-3 text-base font-bold tracking-tight text-ink">
          {selectedCam?.name ?? 'Camera'}
          <span className="font-mono text-[10px] font-bold tracking-[0.2em] text-ink-faint">
            CH {selectedCam?.id ?? 0}
          </span>
        </h2>
        <button
          onClick={onClose}
          className="flex h-9 w-9 items-center justify-center rounded-lg border border-edge text-ink-dim transition-all hover:border-ink-faint hover:text-ink active:scale-90"
        >
          <span className="material-symbols-outlined text-[18px]">close</span>
        </button>
      </div>

      {selectedCam && manifest ? (
        <>
          <div className="flex shrink-0 gap-2 overflow-x-auto px-5 py-2 [scrollbar-width:none] [&::-webkit-scrollbar]:hidden">
            <ToolGroup label="Date">
              <input
                type="date"
                value={effectiveDate}
                min={dates[dates.length - 1]}
                max={dates[0]}
                onChange={(e) => onDateChange(e.target.value)}
                className="min-w-[118px] bg-transparent font-sans text-sm font-semibold text-ink outline-none [color-scheme:dark]"
              />
            </ToolGroup>
            <ToolGroup label="Hr">
              <select
                value={selHour}
                onChange={(e) => onHourChange(e.target.value)}
                className="bg-transparent pr-6 font-sans text-sm font-semibold text-ink outline-none"
              >
                {HOUR_OPTIONS.map((h) => (
                  <option key={h} value={h} className="bg-surface-2">
                    {h}:00
                  </option>
                ))}
              </select>
            </ToolGroup>
            <button
              disabled={videoBusy}
              onClick={() => onRequestVideo(selectedCam.id, effectiveDate, selHour)}
              className="shrink-0 rounded-lg border border-danger/40 bg-danger-soft px-3 font-mono text-[10px] font-bold uppercase tracking-[0.16em] text-danger transition-all hover:bg-danger/20 active:scale-95 disabled:opacity-40"
            >
              ● Record
            </button>
            <button
              disabled={videoBusy}
              onClick={() => onRequestVideo(selectedCam.id, effectiveDate, null)}
              className="shrink-0 rounded-lg border border-warn/40 bg-warn/10 px-3 font-mono text-[10px] font-bold uppercase tracking-[0.16em] text-warn transition-all hover:bg-warn/20 active:scale-95 disabled:opacity-40"
            >
              24h
            </button>
            <ToolGroup label="Show" grow>
              <select
                value={pageSize}
                onChange={(e) => onPageSizeChange(parseInt(e.target.value, 10))}
                className="bg-transparent pr-6 font-sans text-sm font-semibold text-ink outline-none"
              >
                <option value="10" className="bg-surface-2">10</option>
                <option value="50" className="bg-surface-2">50</option>
                <option value="100" className="bg-surface-2">100</option>
                <option value="300" className="bg-surface-2">300</option>
                <option value="0" className="bg-surface-2">All</option>
              </select>
            </ToolGroup>
          </div>

          <div className="flex-1 overflow-y-auto px-4 pb-[calc(64px+env(safe-area-inset-bottom,0px))] md:pb-4">
            {streamingCamId === selectedCam.id ? (
              <LiveStream cameraId={selectedCam.id} className="mb-3 aspect-video" />
            ) : (
              <div className={`relative mb-3 overflow-hidden rounded-xl border border-edge bg-surface-2 ${selectedCam.last_snapshot ? '' : 'hidden'}`}>
                {selectedCam.last_snapshot && (
                  <>
                    <img
                      src={`${imgPath(selectedCam.last_snapshot.image_path)}?_=${Date.now()}`}
                      alt=""
                      className="aspect-video w-full object-cover"
                    />
                    <div className="absolute inset-x-0 bottom-0 flex items-end justify-between bg-gradient-to-t from-black/85 to-transparent px-3 pb-2 pt-6">
                      <span className="font-mono text-[10px] tabular-nums text-ink-dim">
                        {formatTimeShort(selectedCam.last_snapshot.captured_at)}
                      </span>
                      <span className="font-mono text-[10px] font-bold tabular-nums text-ok">
                        {timeAgo(selectedCam.last_snapshot.captured_at)}
                      </span>
                    </div>
                  </>
                )}
              </div>
            )}

            {selectedCam.id !== undefined && (
              <div className="mb-3 flex gap-2">
                <button
                  onClick={() => onToggleStream(selectedCam.id)}
                  className={`rounded-lg px-3 py-1.5 font-mono text-[10px] font-bold uppercase tracking-[0.16em] transition-all active:scale-95 ${
                    streamingCamId === selectedCam.id
                      ? 'border border-danger/40 bg-danger-soft text-danger hover:bg-danger/20'
                      : 'border border-accent/40 bg-accent/10 text-accent hover:bg-accent/20'
                  }`}
                >
                  {streamingCamId === selectedCam.id ? '■ Stop Live' : '▶ Live'}
                </button>
              </div>
            )}

            <div className="mb-3 flex flex-wrap gap-1.5">
              <StatPill label="TOTAL" value={String(selectedCam.total_snapshots)} />
              <StatPill label="INTV" value={`${selectedCam.interval_seconds}s`} />
              <StatPill label="DAYS" value={String(dates.length)} />
              {effectiveDate && dateCount(selectedCam, manifest, effectiveDate) > 0 && (
                <StatPill label={effectiveDate} value={String(dateCount(selectedCam, manifest, effectiveDate))} />
              )}
              {selHour && allSnaps.length > 0 && (
                <StatPill label={`HR ${selHour}:00`} value={String(allSnaps.length)} />
              )}
            </div>

            <div className="grid grid-cols-3 gap-1.5 md:grid-cols-4">
              {allSnaps.length === 0 ? (
                <div className="col-span-full py-10 text-center font-mono text-[11px] uppercase tracking-[0.2em] text-ink-faint">
                  No Captures{selHour ? ` @ ${selHour}:00` : ''}
                </div>
              ) : (
                renderHourHeaders(
                  allSnaps,
                  pageSnaps,
                  effectiveDate,
                  selectedCam.id,
                  onRequestVideo,
                  videoBusy,
                  onOpenLightbox,
                )
              )}
            </div>

            <div className={`items-center justify-center gap-2 pt-3 ${pageSnaps.pages > 1 ? 'flex' : 'hidden'}`}>
              <button
                disabled={curPage <= 1}
                onClick={() => onPageChange(Math.max(1, curPage - 1))}
                className="rounded-lg border border-edge bg-surface-2/60 px-4 py-1.5 font-mono text-[10px] font-bold uppercase tracking-[0.16em] text-ink-dim transition-all hover:border-accent hover:text-accent-strong active:scale-95 disabled:opacity-30"
              >
                Prev
              </button>
              <span className="min-w-[72px] text-center font-mono text-[11px] tabular-nums text-ink-dim">
                {curPage} / {pageSnaps.pages}
              </span>
              <button
                disabled={curPage >= pageSnaps.pages}
                onClick={() => onPageChange(Math.min(pageSnaps.pages, curPage + 1))}
                className="rounded-lg border border-edge bg-surface-2/60 px-4 py-1.5 font-mono text-[10px] font-bold uppercase tracking-[0.16em] text-ink-dim transition-all hover:border-accent hover:text-accent-strong active:scale-95 disabled:opacity-30"
              >
                Next
              </button>
            </div>
          </div>
        </>
      ) : (
        <div className="flex flex-1 flex-col items-center justify-center gap-3 py-16">
          <span className="material-symbols-outlined text-3xl text-ink-faint/50">videocam</span>
          <p className="font-mono text-[11px] uppercase tracking-[0.24em] text-ink-faint">
            Select a camera
          </p>
        </div>
      )}
    </div>
  );
}

function ToolGroup({
  label,
  children,
  grow = false,
}: {
  label: string;
  children: React.ReactNode;
  grow?: boolean;
}) {
  return (
    <label
      className={`flex shrink-0 items-center gap-2 rounded-lg border border-edge bg-surface-2/60 px-2.5 py-1.5 ${
        grow ? 'ml-auto' : ''
      }`}
    >
      <span className="font-mono text-[9px] font-bold uppercase tracking-[0.18em] text-ink-faint">
        {label}
      </span>
      {children}
    </label>
  );
}

function StatPill({ label, value }: { label: string; value: string }) {
  return (
    <span className="rounded-lg border border-edge bg-surface-2/60 px-2.5 py-1 font-mono text-[10px] uppercase tracking-[0.1em] text-ink-faint">
      {label} <span className="ml-1 font-bold tabular-nums text-ink-dim">{value}</span>
    </span>
  );
}

function GridState({ icon, title, detail }: { icon: string; title: string; detail?: string }) {
  return (
    <div className="col-span-full flex flex-col items-center justify-center gap-3 py-24 text-center">
      <span className="material-symbols-outlined text-4xl text-ink-faint/50">{icon}</span>
      <p className="text-sm font-bold uppercase tracking-[0.22em] text-ink-dim">{title}</p>
      {detail && <p className="max-w-md text-xs text-ink-faint">{detail}</p>}
    </div>
  );
}

function LoadingState({ label }: { label: string }) {
  return (
    <div className="col-span-full flex flex-col items-center justify-center gap-4 py-24">
      <div className="h-7 w-7 animate-spin rounded-full border-2 border-edge border-t-accent" />
      <p className="font-mono text-[11px] uppercase tracking-[0.24em] text-ink-faint">{label}</p>
    </div>
  );
}

function VideoLoader() {
  return (
    <div className="fixed inset-0 z-[9999] flex flex-col items-center justify-center bg-canvas/90 backdrop-blur-sm">
      <div className="h-8 w-8 animate-spin rounded-full border-2 border-edge border-t-accent" />
      <p className="mt-4 font-mono text-[11px] uppercase tracking-[0.2em] text-ink-dim">
        Generating timelapse…
      </p>
    </div>
  );
}

function renderHourHeaders(
  all: SnapView[],
  page: PageSnaps,
  date: string,
  cameraId: number,
  requestVideo: (cameraId: number, date: string, hour: string | null) => Promise<void>,
  videoBusy: boolean,
  openLightbox: (items: SnapView[], index: number) => void,
) {
  const byHour = new Map<string, SnapView[]>();
  for (const s of all) {
    const h = getHour(s.captured_at);
    const list = byHour.get(h) || [];
    list.push(s);
    byHour.set(h, list);
  }

  let lastHour: string | null = null;
  let globalIndex = page.start;
  const nodes: React.ReactNode[] = [];

  page.items.forEach((s, i) => {
    const h = getHour(s.captured_at);
    if (h !== lastHour) {
      lastHour = h;
      const hourList = byHour.get(h) || [];
      nodes.push(
        <div className="col-span-full flex items-center gap-2 border-b border-edge-soft px-0.5 pb-1 pt-3" key={`h-${h}`}>
          <span className="font-mono text-[11px] font-bold tracking-[0.16em] text-accent">{h}:00</span>
          <span className="rounded bg-surface-2 px-1.5 py-0.5 font-mono text-[9px] tabular-nums text-ink-dim">
            {hourList.length}
          </span>
          <button
            disabled={videoBusy}
            onClick={() => requestVideo(cameraId, date, h)}
            className="ml-auto rounded-md border border-danger/30 bg-danger-soft px-2.5 py-1 font-mono text-[9px] font-bold uppercase tracking-[0.14em] text-danger transition-all hover:bg-danger/20 active:scale-95 disabled:opacity-40"
          >
            ● Rec
          </button>
        </div>,
      );
    }
    nodes.push(
      <div
        key={`s-${s.captured_at}-${i}`}
        style={{ ['--i' as string]: globalIndex }}
        onClick={() => openLightbox(all, globalIndex)}
        className="relative aspect-video cursor-pointer overflow-hidden rounded-lg border border-transparent bg-surface-2 transition-colors active:border-accent"
      >
        <img src={imgPath(s.image_path)} alt="" loading="lazy" className="h-full w-full object-cover" />
        <div className="pointer-events-none absolute inset-x-0 bottom-0 bg-gradient-to-t from-black/70 to-transparent px-1 pb-1 pt-4 text-right font-mono text-[9px] tabular-nums text-ink-dim">
          {formatTimeShort(s.captured_at)}
        </div>
      </div>,
    );
    globalIndex++;
  });

  return nodes;
}
