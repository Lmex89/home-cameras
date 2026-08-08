import { useEffect, useMemo, useState } from 'react';
import { AmbientBackground } from './Ambient';
import '../styles/tailwind.css';

export type CameraStatus = 'online' | 'recording' | 'offline';

/** An AI detection marker overlay on a camera feed. */
export interface DetectionMarker {
  class_name: string;
  confidence: number;
}

/** A camera feed shown in the dashboard grid. All fields optional except id/name/status. */
export interface CameraFeed {
  id: string;
  name: string;
  location?: string;
  status: CameraStatus;
  streamUrl?: string;
  thumbnailUrl?: string;
  lastSeen?: string;
  resolution?: string;
  fps?: number;
  detections?: DetectionMarker[];
}

/** A sidebar navigation entry. */
export interface SidebarItem {
  id: string;
  label: string;
  icon?: string;
  badge?: number;
}

/** A stat card in the header strip. */
export interface DashboardStat {
  id: string;
  label: string;
  value: number;
  tone: 'accent' | 'ok' | 'warn' | 'danger' | 'dim';
}

/** Props for CameraDashboard. */
export interface CameraDashboardProps {
  cameras: CameraFeed[];
  siteName?: string;
  siteCode?: string;
  sidebarItems?: SidebarItem[];
  activeNav?: string;
  stats?: DashboardStat[];
  systemStatus?: string;
  armed?: boolean;
  onCameraSelect?: (camera: CameraFeed) => void;
  onRecordToggle?: (camera: CameraFeed, recording: boolean) => void;
  onSnapshot?: (camera: CameraFeed) => void;
  onFullscreen?: (camera: CameraFeed) => void;
  onNavSelect?: (item: SidebarItem) => void;
  onRefresh?: () => void;
  onAddCamera?: () => void;
  onArmToggle?: (armed: boolean) => void;
  onSOS?: () => void;
}

/**
 * SecureView — tactical home security center.
 *
 * iOS command-center aesthetic: glass surfaces over a black canvas,
 * system blue actions (#0A84FF), system green safe states (#30D158) and
 * system red alerts (#FF453A). Typography uses the SF Pro system stack
 * with tabular numerals for data. A 2x2 camera grid on desktop collapses
 * to a single feed column on mobile; controls keep 44px touch targets.
 * Every label is data-driven so real camera streams can be wired in later.
 *
 * Args:
 *   cameras: Feeds to render in the grid.
 *   siteName: Brand name (default "SecureView").
 *   siteCode: Short brand code next to the version footer (default "HOME").
 *   sidebarItems: Navigation entries (defaults: Overview/Cameras/Events/Settings).
 *   activeNav: Id of the selected sidebar item.
 *   stats: Header stat cards (defaults: counts computed from cameras).
 *   systemStatus: Sidebar system line (default "All systems nominal").
 *   armed: Whether the security system is armed (emerald indicator).
 *   onCameraSelect: Fired when a camera card is clicked.
 *   onRecordToggle: Fired when a camera's record control toggles.
 *   onSnapshot: Fired when a camera's snapshot control is used.
 *   onFullscreen: Fired when a camera's fullscreen control is used.
 *   onNavSelect: Fired when a sidebar item is selected.
 *   onRefresh: Fired when the header refresh control is used.
 *   onAddCamera: Fired when the "Add camera" control is used.
 *   onArmToggle: Fired when the armed state is toggled in the sidebar.
 *   onSOS: Fired when the emergency SOS control is used.
 *
 * Returns:
 *   The tactical dashboard layout (sidebar + header + camera grid).
 */
export function CameraDashboard({
  cameras,
  siteName = 'SecureView',
  siteCode = 'HOME',
  sidebarItems,
  activeNav,
  stats,
  systemStatus = 'All systems nominal',
  armed = true,
  onCameraSelect,
  onRecordToggle,
  onSnapshot,
  onFullscreen,
  onNavSelect,
  onRefresh,
  onAddCamera,
  onArmToggle,
  onSOS,
}: CameraDashboardProps) {
  const [now, setNow] = useState(() => new Date());
  const [recording, setRecording] = useState<Record<string, boolean>>(() =>
    Object.fromEntries(cameras.map((c) => [c.id, c.status === 'recording'])),
  );

  useEffect(() => {
    const timer = window.setInterval(() => setNow(new Date()), 1000);
    return () => window.clearInterval(timer);
  }, []);

  const navItems = useMemo(
    () =>
      sidebarItems ?? [
        { id: 'overview', label: 'Overview', icon: '◈' },
        { id: 'cameras', label: 'Cameras', icon: '▣' },
        { id: 'events', label: 'Events', icon: '⚠', badge: 3 },
        { id: 'settings', label: 'Settings', icon: '⚙' },
      ],
    [sidebarItems],
  );

  const statCards = useMemo<DashboardStat[]>(
    () =>
      stats ?? [
        { id: 'online', label: 'Online', value: cameras.filter((c) => c.status === 'online').length, tone: 'ok' },
        { id: 'recording', label: 'Recording', value: cameras.filter((c) => c.status === 'recording').length, tone: 'danger' },
        { id: 'offline', label: 'Offline', value: cameras.filter((c) => c.status === 'offline').length, tone: 'dim' },
        { id: 'events', label: 'Events Today', value: cameras.length, tone: 'accent' },
      ],
    [cameras, stats],
  );

  const toggleRecord = (cam: CameraFeed) => {
    setRecording((prev) => {
      const next = { ...prev, [cam.id]: !(prev[cam.id] ?? cam.status === 'recording') };
      onRecordToggle?.(cam, next[cam.id]);
      return next;
    });
  };

  const isRecording = (cam: CameraFeed) => recording[cam.id] ?? cam.status === 'recording';

  return (
    <div className="relative flex h-screen overflow-hidden bg-canvas font-sans text-ink">
      <AmbientBackground />

      <Sidebar
        siteName={siteName}
        siteCode={siteCode}
        items={navItems}
        activeNav={activeNav}
        systemStatus={systemStatus}
        armed={armed}
        onNavSelect={onNavSelect}
        onArmToggle={onArmToggle}
      />

      <main className="relative z-10 flex min-w-0 flex-1 flex-col">
        <Header
          siteName={siteName}
          now={now}
          stats={statCards}
          onRefresh={onRefresh}
          onAddCamera={onAddCamera}
          onSOS={onSOS}
        />

        <section className="flex-1 overflow-y-auto p-4 md:p-6">
          {cameras.length === 0 ? (
            <EmptyState onAddCamera={onAddCamera} />
          ) : (
            <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 md:gap-5">
              {cameras.map((cam) => (
                <CameraCard
                  key={cam.id}
                  cam={cam}
                  recording={isRecording(cam)}
                  onSelect={onCameraSelect}
                  onRecordToggle={() => toggleRecord(cam)}
                  onSnapshot={onSnapshot}
                  onFullscreen={onFullscreen}
                />
              ))}
            </div>
          )}
        </section>
      </main>
    </div>
  );
}

function Sidebar({
  siteName,
  siteCode,
  items,
  activeNav,
  systemStatus,
  armed,
  onNavSelect,
  onArmToggle,
}: {
  siteName: string;
  siteCode: string;
  items: SidebarItem[];
  activeNav?: string;
  systemStatus: string;
  armed: boolean;
  onNavSelect?: (item: SidebarItem) => void;
  onArmToggle?: (armed: boolean) => void;
}) {
  return (
    <aside className="relative z-10 hidden w-60 shrink-0 flex-col border-r border-edge bg-glass backdrop-blur-md md:flex">
      <div className="flex items-center gap-3 border-b border-edge-soft px-5 py-5">
        <span className="flex h-9 w-9 items-center justify-center rounded-lg bg-accent-soft text-accent-strong shadow-[0_0_14px_rgba(10,132,255,0.25)]">
          🛡
        </span>
        <div className="leading-tight">
          <div className="text-sm font-bold tracking-tight text-ink">{siteName}</div>
          <div className="text-[0.55rem] uppercase tracking-[0.22em] text-ink-faint">
            Home Security
          </div>
        </div>
      </div>

      <button
        onClick={() => onArmToggle?.(!armed)}
        className={`mx-4 mt-4 flex items-center gap-2.5 rounded-lg border px-3 py-2.5 text-left transition-all active:scale-95 ${
          armed
            ? 'border-ok/40 bg-ok-soft text-ok'
            : 'border-danger/40 bg-danger-soft text-danger'
        }`}
      >
        <span
          className={`h-1.5 w-1.5 rounded-full ${
            armed ? 'bg-ok shadow-[0_0_8px_rgba(48,209,88,0.8)]' : 'bg-danger shadow-[0_0_8px_rgba(255,69,58,0.8)]'
          }`}
        />
        <span className="flex-1 text-[0.6rem] font-semibold uppercase tracking-[0.18em]">
          {armed ? 'System Armed' : 'System Disarmed'}
        </span>
      </button>

      <nav className="mt-4 flex flex-col gap-1 p-3">
        <div className="px-3 pb-2 pt-1 text-[0.55rem] uppercase tracking-[0.22em] text-ink-faint">
          Monitor
        </div>
        {items.map((item) => {
          const active = item.id === activeNav;
          return (
            <button
              key={item.id}
              onClick={() => onNavSelect?.(item)}
              className={`flex items-center gap-3 rounded-lg px-3 py-2 text-left text-[0.7rem] tracking-wide transition-all active:scale-[0.98] ${
                active
                  ? 'bg-accent-soft text-accent-strong'
                  : 'text-ink-dim hover:bg-surface-2/60 hover:text-ink'
              }`}
            >
              <span className="w-4 text-center">{item.icon}</span>
              <span className="flex-1 uppercase tracking-[0.14em]">{item.label}</span>
              {item.badge !== undefined && item.badge > 0 && (
                <span className="rounded-full bg-danger-soft px-1.5 py-0.5 text-[0.55rem] font-semibold tabular-nums text-danger">
                  {item.badge}
                </span>
              )}
            </button>
          );
        })}
      </nav>

      <div className="mt-auto border-t border-edge-soft p-4">
        <div className="flex items-center gap-2 text-[0.6rem] uppercase tracking-[0.18em] text-ink-faint">
          <span className="h-1.5 w-1.5 rounded-full bg-ok shadow-[0_0_6px_rgba(48,209,88,0.7)]" />
          System
        </div>
        <div className="mt-1.5 text-[0.6rem] leading-relaxed text-ink-dim">{systemStatus}</div>
        <div className="mt-3 text-[0.55rem] tracking-[0.16em] text-ink-faint">
          SECUREVIEW v2.0 · {siteCode}
        </div>
      </div>
    </aside>
  );
}

function Header({
  siteName,
  now,
  stats,
  onRefresh,
  onAddCamera,
  onSOS,
}: {
  siteName: string;
  now: Date;
  stats: DashboardStat[];
  onRefresh?: () => void;
  onAddCamera?: () => void;
  onSOS?: () => void;
}) {
  const time = now.toLocaleTimeString(undefined, { hour12: false });
  const date = now.toLocaleDateString(undefined, { weekday: 'short', month: 'short', day: 'numeric' });

  return (
    <header className="border-b border-edge-soft px-4 py-4 md:px-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <div className="flex items-center gap-2 text-[0.55rem] uppercase tracking-[0.28em] text-accent">
            <span className="inline-block h-1 w-1 rounded-full bg-accent shadow-[0_0_6px_rgba(10,132,255,0.8)]" />
            Security Center
          </div>
          <h1 className="mt-1 text-lg font-bold tracking-tight text-ink">{siteName} Dashboard</h1>
        </div>

        <div className="flex items-center gap-2">
          <div className="hidden pr-3 text-right sm:block">
            <div className="text-sm font-semibold tabular-nums text-ink">{time}</div>
            <div className="text-[0.55rem] uppercase tracking-[0.18em] text-ink-faint">{date}</div>
          </div>
          <button
            onClick={onRefresh}
            className="flex min-h-11 items-center rounded-lg border border-edge px-3.5 text-[0.6rem] font-semibold uppercase tracking-[0.16em] text-ink-dim transition-all hover:border-accent hover:text-accent-strong active:scale-95 md:min-h-0 md:py-2"
          >
            ⟳ Sync
          </button>
          <button
            onClick={onSOS}
            className="flex min-h-11 items-center rounded-lg border border-danger bg-danger px-3.5 text-[0.6rem] font-bold uppercase tracking-[0.16em] text-white shadow-[0_0_16px_rgba(255,69,58,0.35)] transition-all hover:bg-danger/90 active:scale-95 md:min-h-0 md:py-2"
          >
            SOS
          </button>
          <button
            onClick={onAddCamera}
            className="flex min-h-11 items-center rounded-lg bg-accent px-3.5 text-[0.6rem] font-bold uppercase tracking-[0.16em] text-canvas transition-all hover:bg-accent-strong active:scale-95 md:min-h-0 md:py-2"
          >
            + Add Camera
          </button>
        </div>
      </div>

      <div className="mt-4 grid grid-cols-2 gap-3 md:grid-cols-4">
        {stats.map((s) => (
          <StatCard key={s.id} stat={s} />
        ))}
      </div>
    </header>
  );
}

const STAT_TONES: Record<DashboardStat['tone'], string> = {
  accent: 'text-accent-strong',
  ok: 'text-ok',
  warn: 'text-warn',
  danger: 'text-danger',
  dim: 'text-ink-faint',
};

function StatCard({ stat }: { stat: DashboardStat }) {
  return (
    <div className="rounded-xl border border-edge bg-glass px-4 py-3 backdrop-blur-md">
      <div className="text-[0.55rem] uppercase tracking-[0.22em] text-ink-faint">{stat.label}</div>
      <div className={`mt-1 text-xl font-bold tabular-nums ${STAT_TONES[stat.tone]}`}>
        {stat.value}
      </div>
    </div>
  );
}

function CameraCard({
  cam,
  recording,
  onSelect,
  onRecordToggle,
  onSnapshot,
  onFullscreen,
}: {
  cam: CameraFeed;
  recording: boolean;
  onSelect?: (cam: CameraFeed) => void;
  onRecordToggle?: (cam: CameraFeed) => void;
  onSnapshot?: (cam: CameraFeed) => void;
  onFullscreen?: (cam: CameraFeed) => void;
}) {
  const lastSeen = cam.lastSeen ? ` · ${timeAgo(cam.lastSeen)}` : '';
  const meta = [cam.resolution, cam.fps ? `${cam.fps} fps` : null].filter(Boolean).join(' · ');

  return (
    <div
      onClick={() => onSelect?.(cam)}
      className="group cursor-pointer overflow-hidden rounded-xl border border-edge bg-glass backdrop-blur-md transition-all hover:border-accent/50 hover:shadow-[0_0_28px_rgba(10,132,255,0.14)] active:scale-[0.99]"
    >
      <div className="relative aspect-video overflow-hidden bg-canvas">
        {cam.thumbnailUrl ? (
          <img src={cam.thumbnailUrl} alt={cam.name} className="h-full w-full object-cover" />
        ) : (
          <FeedPlaceholder />
        )}
        <div className="pointer-events-none absolute inset-0 bg-[linear-gradient(0deg,rgba(142,142,147,0.05)_1px,transparent_1px)] bg-[size:100%_4px] opacity-50" />

        <span className="absolute left-2.5 top-2.5 rounded-md bg-canvas/70 px-1.5 py-0.5 text-[0.5rem] font-semibold tracking-[0.18em] text-ink-faint backdrop-blur-sm">
          CAM {cam.id}
        </span>
        <StatusBadge status={recording ? 'recording' : cam.status} />

        {cam.detections && cam.detections.length > 0 && (
          <div className="absolute right-2.5 top-9 flex flex-col items-end gap-1">
            {cam.detections.slice(0, 3).map((d) => (
              <span
                key={d.class_name}
                className="rounded-md border border-accent/50 bg-canvas/70 px-1.5 py-0.5 text-[0.5rem] font-semibold uppercase tracking-[0.12em] text-accent-strong backdrop-blur-sm"
              >
                {d.class_name} <span className="tabular-nums text-ink-dim">{(d.confidence * 100).toFixed(0)}%</span>
              </span>
            ))}
          </div>
        )}

        <div className="absolute inset-x-0 bottom-0 bg-gradient-to-t from-canvas/95 to-transparent px-3 pb-2 pt-8">
          <div className="truncate text-xs font-bold tracking-tight text-ink">{cam.name}</div>
          <div className="truncate text-[0.55rem] tabular-nums text-ink-faint">
            {cam.location || '—'}
            {lastSeen}
          </div>
          {meta && <div className="mt-0.5 text-[0.5rem] tabular-nums text-ink-faint">{meta}</div>}
        </div>
      </div>

      <div className="flex items-center gap-2 border-t border-edge-soft px-3 py-2.5">
        <ControlButton
          title={recording ? 'Stop recording' : 'Start recording'}
          onClick={() => onRecordToggle?.(cam)}
          active={recording}
          activeClass="border-danger/60 bg-danger-soft text-danger shadow-[0_0_12px_rgba(255,69,58,0.25)]"
        >
          ●
        </ControlButton>
        <ControlButton title="Snapshot" onClick={() => onSnapshot?.(cam)}>
          ◉
        </ControlButton>
        <ControlButton title="Fullscreen" onClick={() => onFullscreen?.(cam)}>
          ⛶
        </ControlButton>
        {cam.streamUrl && (
          <a
            href={cam.streamUrl}
            onClick={(e) => e.stopPropagation()}
            className="ml-auto py-2 text-[0.55rem] uppercase tracking-[0.18em] text-ink-faint transition-colors hover:text-accent"
          >
            Live ↗
          </a>
        )}
      </div>
    </div>
  );
}

function ControlButton({
  title,
  onClick,
  active = false,
  activeClass,
  children,
}: {
  title: string;
  onClick?: () => void;
  active?: boolean;
  activeClass?: string;
  children: string;
}) {
  return (
    <button
      title={title}
      onClick={(e) => {
        e.stopPropagation();
        onClick?.();
      }}
      className={`flex h-11 w-11 items-center justify-center rounded-lg border text-sm transition-all active:scale-90 md:h-9 md:w-9 ${
        active && activeClass
          ? activeClass
          : 'border-edge bg-canvas/40 text-ink-dim hover:border-accent hover:text-accent-strong'
      }`}
    >
      {children}
    </button>
  );
}

function FeedPlaceholder() {
  return (
    <>
      <div className="absolute inset-0 bg-gradient-to-br from-surface-2 via-surface to-canvas" />
      <div className="absolute inset-0 flex items-center justify-center">
        <span className="text-2xl text-ink-faint/40">▣</span>
      </div>
    </>
  );
}

function StatusBadge({ status }: { status: CameraStatus }) {
  if (status === 'recording') {
    return (
      <span className="absolute right-2.5 top-2.5 flex items-center gap-1.5 rounded-md bg-warn/15 px-1.5 py-0.5 text-[0.5rem] font-bold uppercase tracking-[0.16em] text-warn backdrop-blur-sm">
        <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-warn" />
        Rec
      </span>
    );
  }
  if (status === 'offline') {
    return (
      <span className="absolute right-2.5 top-2.5 rounded-md bg-canvas/70 px-1.5 py-0.5 text-[0.5rem] font-semibold uppercase tracking-[0.16em] text-ink-faint backdrop-blur-sm">
        Offline
      </span>
    );
  }
  return (
    <span className="absolute right-2.5 top-2.5 flex items-center gap-1.5 rounded-md bg-danger-soft px-1.5 py-0.5 text-[0.5rem] font-bold uppercase tracking-[0.16em] text-danger shadow-[0_0_10px_rgba(255,69,58,0.25)] backdrop-blur-sm">
      <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-danger" />
      Live
    </span>
  );
}

function EmptyState({ onAddCamera }: { onAddCamera?: () => void }) {
  return (
    <div className="flex h-full flex-col items-center justify-center gap-4 text-center">
      <span className="text-4xl text-ink-faint/40">▣</span>
      <p className="text-xs uppercase tracking-[0.22em] text-ink-dim">No cameras configured</p>
      {onAddCamera && (
        <button
          onClick={onAddCamera}
          className="rounded-lg bg-accent px-4 py-2.5 text-[0.6rem] font-bold uppercase tracking-[0.16em] text-canvas transition-all hover:bg-accent-strong active:scale-95"
        >
          + Add First Camera
        </button>
      )}
    </div>
  );
}

function timeAgo(iso: string): string {
  const diff = Math.floor((Date.now() - new Date(iso).getTime()) / 1000);
  if (diff < 60) return `${diff}s`;
  if (diff < 3600) return `${Math.floor(diff / 60)}m`;
  if (diff < 86400) return `${Math.floor(diff / 3600)}h`;
  return `${Math.floor(diff / 86400)}d`;
}
