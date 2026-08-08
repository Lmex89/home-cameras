import '../styles/tailwind.css';

export type MobileCameraStatus = 'live' | 'offline';

/** A camera feed card in the mobile dashboard. */
export interface MobileCamera {
  id: string;
  name: string;
  status: MobileCameraStatus;
  thumbnailUrl?: string;
  streamUrl?: string;
  ptz?: boolean;
  mic?: boolean;
  lastSeen?: string;
}

/** A bottom navigation entry (icon is a Material Symbols name). */
export interface MobileNavItem {
  id: string;
  label: string;
  icon: string;
  badge?: number;
}

/** Props for MobileDashboard. */
export interface MobileDashboardProps {
  cameras: MobileCamera[];
  siteName?: string;
  armed?: boolean;
  armedLabel?: string;
  armedDetail?: string;
  navItems?: MobileNavItem[];
  activeNav?: string;
  notifications?: number;
  onCameraSelect?: (camera: MobileCamera) => void;
  onPtz?: (camera: MobileCamera, direction: 'left' | 'right') => void;
  onMic?: (camera: MobileCamera) => void;
  onArmAll?: () => void;
  onSOS?: () => void;
  onNavSelect?: (item: MobileNavItem) => void;
  onNotifications?: () => void;
}

/**
 * SecureView — mobile home security center.
 *
 * The handheld command deck follows iOS dark-mode patterns: a large-title
 * glass app bar, system-color cards, rounded 10–12px corners, and a bottom
 * tab bar with system blue accents. Live feed cards use iOS-style overlays
 * for PTZ and mic controls. Cards enter with a staggered reveal; every
 * control keeps a 44px touch target. Labels are data-driven props so real
 * streams can be wired in later.
 *
 * Args:
 *   cameras: Feeds to render (single column, mobile-first).
 *   siteName: Brand name in the app bar (default "SecureView").
 *   armed: Whether the system is armed (default true).
 *   armedLabel: Status card headline (default "System Armed").
 *   armedDetail: Status card sub-line (default "All Sensors Active").
 *   navItems: Bottom navigation entries (defaults: Dashboard/Cameras/Events/Settings).
 *   activeNav: Id of the selected bottom nav item.
 *   notifications: Unread notification count shown on the bell.
 *   onCameraSelect: Fired when a feed card is tapped.
 *   onPtz: Fired when a PTZ arrow is tapped (direction).
 *   onMic: Fired when the mic overlay is tapped.
 *   onArmAll: Fired when the "Arm All" action is tapped.
 *   onSOS: Fired when the emergency action is tapped.
 *   onNavSelect: Fired when a bottom nav item is selected.
 *   onNotifications: Fired when the bell is tapped.
 *
 * Returns:
 *   The mobile dashboard layout (app bar + status + feeds + bottom nav).
 */
export function MobileDashboard({
  cameras,
  siteName = 'SecureView',
  armed = true,
  armedLabel = 'System Armed',
  armedDetail = 'All Sensors Active',
  navItems,
  activeNav,
  notifications = 0,
  onCameraSelect,
  onPtz,
  onMic,
  onArmAll,
  onSOS,
  onNavSelect,
  onNotifications,
}: MobileDashboardProps) {
  const items = navItems ?? [
    { id: 'dashboard', label: 'Dashboard', icon: 'dashboard' },
    { id: 'cameras', label: 'Cameras', icon: 'videocam' },
    { id: 'events', label: 'Events', icon: 'event_note' },
    { id: 'settings', label: 'Settings', icon: 'settings' },
  ];

  return (
    <div className="relative flex h-dvh flex-col overflow-hidden bg-canvas font-sans text-ink antialiased">
      <div
        className="pointer-events-none absolute inset-0"
        style={{
          backgroundImage:
            'radial-gradient(420px 320px at 12% 0%, rgba(10,132,255,0.08), transparent 60%),' +
            'radial-gradient(520px 420px at 100% 100%, rgba(255,69,58,0.05), transparent 60%),' +
            'repeating-linear-gradient(0deg, rgba(142,142,147,0.03) 0 1px, transparent 1px 44px),' +
            'repeating-linear-gradient(90deg, rgba(142,142,147,0.03) 0 1px, transparent 1px 44px)',
        }}
      />

      <header className="mobile-header flex items-center justify-between border-b border-edge bg-glass/80 px-4 backdrop-blur-xl">
        <div className="flex-1">
          <div className="text-[0.65rem] font-semibold uppercase tracking-[0.18em] text-ink-dim">
            Security
          </div>
          <div className="text-[1.35rem] font-bold tracking-tight text-ink">
            {siteName}
          </div>
        </div>
        <button
          onClick={onNotifications}
          className="relative flex h-11 w-11 items-center justify-center rounded-full text-ink-dim transition-colors hover:text-accent active:scale-95"
        >
          <span className="material-symbols-outlined text-[22px]">notifications</span>
          {notifications > 0 && (
            <span className="absolute right-1.5 top-1.5 flex h-4 min-w-4 items-center justify-center rounded-full bg-danger px-1 text-[9px] font-bold tabular-nums text-white">
              {notifications}
            </span>
          )}
        </button>
      </header>

      <main className="mobile-main mx-auto flex max-w-full flex-1 flex-col gap-4 px-4">
        <StatusCard
          armed={armed}
          label={armedLabel}
          detail={armedDetail}
        />

        <div className="grid grid-cols-2 gap-3">
          <button
            onClick={onArmAll}
            className="flex h-12 items-center justify-center gap-2 rounded-[10px] bg-accent text-sm font-semibold text-white transition-all hover:bg-accent-strong active:scale-[0.97]"
          >
            <span className="material-symbols-outlined text-[18px]">verified_user</span>
            Arm All
          </button>
          <button
            onClick={onSOS}
            className="flex h-12 items-center justify-center gap-2 rounded-[10px] bg-danger text-sm font-semibold text-white transition-all hover:bg-danger/90 active:scale-[0.97]"
          >
            <span className="material-symbols-outlined text-[18px]">warning</span>
            SOS
          </button>
        </div>

        <section className="flex flex-col gap-3">
          <h3 className="text-[1.05rem] font-semibold tracking-tight text-ink">Live Feeds</h3>

          {cameras.length === 0 ? (
            <div className="flex flex-col items-center gap-3 rounded-[12px] border border-edge bg-surface py-12 text-center">
              <span className="material-symbols-outlined text-3xl text-ink-faint">videocam_off</span>
              <p className="text-xs text-ink-dim">No cameras configured</p>
            </div>
          ) : (
            cameras.map((cam, i) => (
              <CameraFeedCard
                key={cam.id}
                cam={cam}
                index={i}
                onSelect={onCameraSelect}
                onPtz={onPtz}
                onMic={onMic}
              />
            ))
          )}
        </section>
      </main>

      <nav className="mobile-nav flex items-center justify-around border-t border-edge bg-glass/80 px-2 pt-1 pb-[calc(0.25rem+env(safe-area-inset-bottom,0px))] backdrop-blur-xl">
        {items.map((item) => {
          const active = item.id === activeNav;
          return (
            <button
              key={item.id}
              onClick={() => onNavSelect?.(item)}
              className={`relative flex min-h-11 min-w-[64px] flex-1 flex-col items-center justify-center rounded-[10px] px-2 py-1 transition-all duration-150 active:scale-95 ${
                active ? 'text-accent' : 'text-ink-dim hover:text-ink'
              }`}
            >
              <span className="material-symbols-outlined mb-0.5 text-[22px]">{item.icon}</span>
              <span className="whitespace-nowrap text-[10px] font-medium">
                {item.label}
              </span>
              {item.badge !== undefined && item.badge > 0 && (
                <span className="absolute right-2 top-1 flex h-4 min-w-4 items-center justify-center rounded-full bg-danger px-1 text-[9px] font-bold tabular-nums text-white">
                  {item.badge}
                </span>
              )}
            </button>
          );
        })}
      </nav>
    </div>
  );
}

function StatusCard({
  armed,
  label,
  detail,
}: {
  armed: boolean;
  label: string;
  detail: string;
}) {
  return (
    <div className="flex animate-feed-in items-center justify-between rounded-[12px] border border-edge bg-surface p-4 shadow-sm">
      <div className="flex items-center gap-4">
        <div className="relative flex h-12 w-12 items-center justify-center rounded-full bg-ok-soft">
          <div className="absolute inset-0 animate-ping rounded-full bg-ok/30" style={{ animationDuration: '2.2s' }} />
          <span className="material-symbols-outlined relative text-ok">verified_user</span>
        </div>
        <div>
          <h2 className="text-base font-semibold tracking-tight text-ink">{label}</h2>
          <p className="text-sm text-ink-dim">{detail}</p>
        </div>
      </div>
      <span
        className={`h-2.5 w-2.5 rounded-full ${armed ? 'bg-ok shadow-[0_0_6px_rgba(48,209,88,0.8)]' : 'bg-danger shadow-[0_0_6px_rgba(255,69,58,0.8)]'}`}
      />
    </div>
  );
}

function CameraFeedCard({
  cam,
  index,
  onSelect,
  onPtz,
  onMic,
}: {
  cam: MobileCamera;
  index: number;
  onSelect?: (cam: MobileCamera) => void;
  onPtz?: (cam: MobileCamera, direction: 'left' | 'right') => void;
  onMic?: (cam: MobileCamera) => void;
}) {
  const live = cam.status === 'live';
  const lastSeen = cam.lastSeen ? ` · ${timeAgo(cam.lastSeen)}` : '';

  return (
    <div
      onClick={() => onSelect?.(cam)}
      className="relative max-w-full animate-feed-in overflow-hidden rounded-[12px] border border-edge bg-surface shadow-sm transition-all active:scale-[0.98]"
      style={{ animationDelay: `${180 + index * 90}ms` }}
    >
      <div className="absolute inset-x-0 top-0 z-10 flex items-center justify-between bg-gradient-to-b from-black/60 to-transparent p-3">
        <span className="text-sm font-semibold text-white drop-shadow-sm">{cam.name}</span>
        {live ? (
          <span className="flex animate-badge-in items-center gap-1 rounded-full bg-danger px-2.5 py-1 text-[10px] font-semibold uppercase tracking-[0.08em] text-white">
            <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-white" />
            Live
          </span>
        ) : (
          <span className="animate-badge-in rounded-full bg-black/40 px-2.5 py-1 text-[10px] font-semibold uppercase tracking-[0.08em] text-ink-dim backdrop-blur-sm">
            Offline
          </span>
        )}
      </div>

      <div className="relative aspect-video w-full max-w-full overflow-hidden bg-surface-2">
        {cam.thumbnailUrl ? (
          <img
            src={cam.thumbnailUrl}
            alt={cam.name}
            className="h-full w-full max-w-full object-cover"
            onError={(e) => ((e.target as HTMLImageElement).style.display = 'none')}
          />
        ) : (
          <div className="absolute inset-0 flex items-center justify-center">
            <span className="material-symbols-outlined text-3xl text-ink-faint/50">videocam</span>
          </div>
        )}
        {!live && (
          <div className="absolute inset-0 flex items-center justify-center bg-black/40 backdrop-blur-[2px]">
            <span className="text-[0.65rem] uppercase tracking-[0.16em] text-ink-dim">
              No Signal{lastSeen}
            </span>
          </div>
        )}
      </div>

      <div className="absolute bottom-3 right-3 z-10 flex gap-2">
        {cam.ptz && (
          <div className="flex items-center rounded-full border border-white/20 bg-black/40 px-1 backdrop-blur-md">
            <button
              onClick={(e) => {
                e.stopPropagation();
                onPtz?.(cam, 'left');
              }}
              className="flex h-10 w-10 items-center justify-center rounded-full text-white transition-colors hover:text-accent active:scale-95"
            >
              <span className="material-symbols-outlined text-[20px]">chevron_left</span>
            </button>
            <span className="px-1 text-[10px] font-semibold uppercase tracking-[0.1em] text-white/80">
              PTZ
            </span>
            <button
              onClick={(e) => {
                e.stopPropagation();
                onPtz?.(cam, 'right');
              }}
              className="flex h-10 w-10 items-center justify-center rounded-full text-white transition-colors hover:text-accent active:scale-95"
            >
              <span className="material-symbols-outlined text-[20px]">chevron_right</span>
            </button>
          </div>
        )}
        {cam.mic && (
          <button
            onClick={(e) => {
              e.stopPropagation();
              onMic?.(cam);
            }}
            className="flex h-10 w-10 items-center justify-center rounded-full border border-white/20 bg-black/40 text-white backdrop-blur-md transition-colors hover:text-accent active:scale-95"
          >
            <span className="material-symbols-outlined text-[20px]">mic</span>
          </button>
        )}
      </div>
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
