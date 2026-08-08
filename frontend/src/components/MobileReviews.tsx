import { useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import { imgPath } from '../api/client';
import type { PendingReviewItem } from '../api/types';
import { Lightbox } from './Lightbox';
import type { LightboxItem } from './Lightbox';
import { parseDetections, reasonClass, reasonLabel, tagClass } from '../utils/detections';
import { formatTime, formatTimeShort, todayStr } from '../utils/format';

/** A flattened detection row for the mobile detections browser. */
export interface MobileDetectionRow {
  analysis_id: number;
  camera_name: string;
  captured_at: string;
  image_path: string;
  review_required: boolean;
  review_reason: string | null;
  objects_json: string | null;
}

/** A mobile header stat. */
export interface MobileReviewStat {
  id: string;
  label: string;
  value: number;
  tone: 'accent' | 'ok' | 'warn' | 'danger';
}

/** Props for MobileReviews. */
export interface MobileReviewsProps {
  pending: PendingReviewItem[];
  detections: MobileDetectionRow[];
  tab: 'reviews' | 'detections';
  stats: MobileReviewStat[];
  filterDate: string;
  detectionsLoading: boolean;
  hasMore: boolean;
  loadMoreLoading: boolean;
  siteName?: string;
  onTabChange: (tab: 'reviews' | 'detections') => void;
  onDateChange: (date: string) => void;
  onLoadMore: () => void;
  onRefresh: () => void;
  onApprove: (analysisId: number) => void;
  onDismiss: (analysisId: number) => void;
}

/**
 * SecureView — mobile review console.
 *
 * The handheld review deck in the same command-center language as
 * MobileDashboard: glass app bar and bottom nav on a phone-width column,
 * a stat strip, Pending/Detections tabs, and single-column review cards
 * with 44px approve/dismiss targets. Cards enter with a staggered reveal
 * and open a swipeable lightbox.
 *
 * Args:
 *   pending: Flagged analyses awaiting review.
 *   detections: Flattened detection rows for the browser tab.
 *   tab: Active tab.
 *   stats: Header stat chips (pending/person/unexpected/today).
 *   filterDate: Date driving the detections query.
 *   detectionsLoading: Whether the detections tab is fetching.
 *   hasMore: Whether more detections can be loaded.
 *   loadMoreLoading: Whether a Load More request is in flight.
 *   siteName: Brand name in the app bar (default "SecureView").
 *   onTabChange: Fired when a tab is selected.
 *   onDateChange: Fired when the detections date changes.
 *   onLoadMore: Fired when "Load More" is tapped.
 *   onRefresh: Fired when the refresh control is tapped.
 *   onApprove: Fired when a review is approved.
 *   onDismiss: Fired when a review is dismissed.
 *
 * Returns:
 *   The mobile review layout (app bar + stats + tabs + lists).
 */
export function MobileReviews({
  pending,
  detections,
  tab,
  stats,
  filterDate,
  detectionsLoading,
  hasMore,
  loadMoreLoading,
  siteName = 'SecureView',
  onTabChange,
  onDateChange,
  onLoadMore,
  onRefresh,
  onApprove,
  onDismiss,
}: MobileReviewsProps) {
  const [lb, setLb] = useState<{ items: LightboxItem[]; index: number } | null>(null);

  const reviewItems = useMemo(
    () =>
      pending.map((r) => ({
        image_path: r.image_path,
        label: `${r.camera_name || 'Camera ' + r.camera_id} — ${formatTime(r.captured_at)}`,
      })),
    [pending],
  );

  const detectionItems = useMemo(
    () =>
      detections.map((d) => ({
        image_path: d.image_path,
        label: `${d.camera_name} — ${formatTimeShort(d.captured_at)}`,
      })),
    [detections],
  );

  return (
    <div className="relative flex h-dvh flex-col overflow-hidden bg-canvas font-sans text-ink antialiased">
      <div
        className="pointer-events-none absolute inset-0"
        style={{
          backgroundImage:
            'radial-gradient(420px 320px at 12% 0%, rgba(10,132,255,0.1), transparent 60%),' +
            'radial-gradient(520px 420px at 100% 100%, rgba(255,69,58,0.06), transparent 60%),' +
            'repeating-linear-gradient(0deg, rgba(142,142,147,0.035) 0 1px, transparent 1px 44px),' +
            'repeating-linear-gradient(90deg, rgba(142,142,147,0.035) 0 1px, transparent 1px 44px)',
        }}
      />

      <header className="mobile-header flex items-center justify-between border-b border-edge bg-glass/70 px-3 backdrop-blur-md">
        <Link
          to="/"
          className="flex h-11 w-11 items-center justify-start rounded-full text-accent-strong transition-colors active:scale-90"
        >
          <span className="material-symbols-outlined text-[20px]">chevron_left</span>
        </Link>
        <div className="flex-1 truncate text-center text-xl font-bold tracking-tight text-accent-strong">
          {siteName}
          <span className="font-mono text-[10px] font-bold uppercase tracking-[0.2em] text-ink-faint">
            {' '}
            · review
          </span>
        </div>
        <button
          onClick={onRefresh}
          className="flex h-11 w-11 items-center justify-end rounded-full text-ink-dim transition-all hover:text-accent-strong active:scale-90"
        >
          <span className="material-symbols-outlined text-[20px]">refresh</span>
        </button>
      </header>

      <main className="mobile-main mx-auto flex flex-1 flex-col gap-5 px-4">
        <div className="flex animate-feed-in gap-2 overflow-x-auto pb-0.5 [scrollbar-width:none] [&::-webkit-scrollbar]:hidden">
          {stats.map((s) => (
            <div
              key={s.id}
              className="flex shrink-0 flex-col rounded-xl border border-edge bg-surface-high px-4 py-2.5"
            >
              <span className="whitespace-nowrap text-[0.55rem] uppercase tracking-[0.22em] text-ink-faint">{s.label}</span>
              <span className={`text-xl font-bold tabular-nums ${STAT_TONES[s.tone]}`}>{s.value}</span>
            </div>
          ))}
        </div>

        <div className="flex animate-feed-in gap-2 rounded-xl border border-edge bg-glass p-1 backdrop-blur-md" style={{ animationDelay: '80ms' }}>
          <TabButton active={tab === 'reviews'} label="Pending" count={pending.length} onClick={() => onTabChange('reviews')} />
          <TabButton active={tab === 'detections'} label="Detections" count={detections.length} onClick={() => onTabChange('detections')} />
        </div>

        {tab === 'reviews' ? (
          <section className="flex flex-col gap-3">
            {pending.length === 0 ? (
              <EmptyState icon="task_alt" message="No pending reviews" />
            ) : (
              pending.map((r, i) => (
                <ReviewCard
                  key={r.analysis_id}
                  item={r}
                  index={i}
                  onApprove={() => onApprove(r.analysis_id)}
                  onDismiss={() => onDismiss(r.analysis_id)}
                  onView={() => setLb({ items: reviewItems, index: i })}
                />
              ))
            )}
          </section>
        ) : (
          <section className="flex animate-feed-in flex-col gap-3" style={{ animationDelay: '120ms' }}>
            <label className="flex items-center gap-3 rounded-xl border border-edge bg-surface-high px-4 py-2.5">
              <span className="font-mono text-[10px] font-bold uppercase tracking-[0.2em] text-ink-faint">
                Date
              </span>
              <input
                type="date"
                value={filterDate}
                max={todayStr()}
                onChange={(e) => onDateChange(e.target.value)}
                className="min-h-11 flex-1 bg-transparent font-sans text-sm text-ink outline-none [color-scheme:dark] focus:text-accent-strong"
              />
            </label>

            {detectionsLoading ? (
              <div className="flex items-center justify-center py-16">
                <div className="h-6 w-6 animate-spin rounded-full border-2 border-edge border-t-accent" />
              </div>
            ) : detections.length === 0 ? (
              <EmptyState icon="search_off" message="No detections match filters" />
            ) : (
              <>
                <div className="grid grid-cols-2 gap-2 sm:gap-3">
                  {detections.map((d, i) => (
                    <DetectionCard
                      key={d.analysis_id}
                      item={d}
                      index={i}
                      onView={() => setLb({ items: detectionItems, index: i })}
                    />
                  ))}
                </div>
                {hasMore && (
                  <button
                    onClick={onLoadMore}
                    disabled={loadMoreLoading}
                    className="flex h-11 items-center justify-center gap-2 rounded-xl border border-edge font-mono text-[10px] font-bold uppercase tracking-[0.2em] text-ink-dim transition-all hover:border-accent hover:text-accent-strong active:scale-95 disabled:opacity-50"
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
                )}
              </>
            )}
          </section>
        )}
      </main>

      <nav className="mobile-nav flex items-center justify-around border-t border-edge bg-glass/70 px-2 py-1.5 backdrop-blur-md">
        <NavButton label="Dashboard" icon="dashboard" to="/" />
        <NavButton label="Cameras" icon="videocam" />
        <NavButton label="Review" icon="event_note" active />
        <NavButton label="Settings" icon="settings" />
      </nav>

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

const STAT_TONES: Record<MobileReviewStat['tone'], string> = {
  accent: 'text-accent-strong',
  ok: 'text-ok',
  warn: 'text-warn',
  danger: 'text-danger',
};

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
      className={`flex min-h-11 flex-1 items-center justify-center gap-2 whitespace-nowrap rounded-lg font-mono text-[10px] font-bold uppercase tracking-[0.16em] transition-all active:scale-95 ${
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

function ReviewCard({
  item,
  index,
  onApprove,
  onDismiss,
  onView,
}: {
  item: PendingReviewItem;
  index: number;
  onApprove: () => void;
  onDismiss: () => void;
  onView: () => void;
}) {
  const detections = parseDetections(item.objects_json);
  const tags = detections.map((d) => (
    <span key={d.class_name} className={`rounded-md border px-1.5 py-0.5 text-[10px] ${tagClass(d.class_name)}`}>
      {d.class_name}{' '}
      <span className="tabular-nums text-ink-faint">{(d.confidence * 100).toFixed(0)}%</span>
    </span>
  ));

  return (
    <div
      className="animate-feed-in overflow-hidden rounded-xl border border-edge bg-glass backdrop-blur-md"
      style={{ animationDelay: `${140 + index * 70}ms` }}
    >
      <div className="relative h-40 w-full overflow-hidden bg-gradient-to-br from-surface-2 via-surface to-canvas">
        <img
          src={imgPath(item.image_path)}
          alt=""
          className="h-full w-full object-cover"
          onClick={onView}
          onError={(e) => ((e.target as HTMLImageElement).style.display = 'none')}
        />
        <div className="absolute inset-x-0 top-0 flex items-start justify-between bg-gradient-to-b from-black/70 to-transparent p-3">
          <span className="text-sm font-semibold text-ink drop-shadow-md">
            {item.camera_name || `Camera ${item.camera_id}`}
          </span>
          <span className={`rounded-md px-1.5 py-0.5 text-[9px] font-bold uppercase tracking-[0.14em] ${reasonClass(item.review_reason) || 'bg-surface-high text-ink-dim'}`}>
            {reasonLabel(item.review_reason)}
          </span>
        </div>
        <div className="absolute inset-x-0 bottom-0 bg-gradient-to-t from-black/70 to-transparent p-3">
          <span className="font-mono text-[10px] tabular-nums text-ink-dim">
            {formatTime(item.captured_at)}
            {item.person_count > 0 && (
              <span className="ml-2 font-bold text-warn">{item.person_count} person(s)</span>
            )}
          </span>
        </div>
      </div>

      {tags.length > 0 && (
        <div className="flex flex-wrap gap-1.5 border-t border-edge-soft px-3 py-2.5">{tags}</div>
      )}

      <div className="flex items-center gap-2 border-t border-edge-soft px-3 py-2.5">
        <button
          onClick={onView}
          className="flex h-11 flex-1 items-center justify-center gap-2 whitespace-nowrap rounded-lg border border-edge font-mono text-[9px] font-bold uppercase tracking-[0.16em] text-ink-dim transition-all hover:border-accent hover:text-accent-strong active:scale-95 sm:text-[10px]"
        >
          <span className="material-symbols-outlined text-[16px]">zoom_in</span>
          View
        </button>
        <button
          onClick={onApprove}
          className="flex h-11 flex-1 items-center justify-center gap-2 whitespace-nowrap rounded-lg border border-ok/50 bg-ok-soft font-mono text-[9px] font-bold uppercase tracking-[0.16em] text-ok transition-all hover:bg-ok hover:text-canvas active:scale-95 sm:text-[10px]"
        >
          <span className="material-symbols-outlined text-[16px]">check</span>
          Approve
        </button>
        <button
          onClick={onDismiss}
          className="flex h-11 flex-1 items-center justify-center gap-2 whitespace-nowrap rounded-lg border border-edge font-mono text-[9px] font-bold uppercase tracking-[0.16em] text-ink-dim transition-all hover:border-danger hover:text-danger active:scale-95 sm:text-[10px]"
        >
          <span className="material-symbols-outlined text-[16px]">close</span>
          Dismiss
        </button>
      </div>
    </div>
  );
}

function DetectionCard({
  item,
  index,
  onView,
}: {
  item: MobileDetectionRow;
  index: number;
  onView: () => void;
}) {
  const objs = parseDetections(item.objects_json);
  const tags = objs
    .slice(0, 2)
    .map((o) => (
      <span key={o.class_name} className={`rounded-md border px-1.5 py-0.5 text-[9px] ${tagClass(o.class_name)}`}>
        {o.class_name}
      </span>
    ));

  return (
    <div
      className="animate-feed-in overflow-hidden rounded-xl border border-edge bg-glass backdrop-blur-md transition-all active:scale-[0.97]"
      style={{ animationDelay: `${140 + index * 50}ms` }}
    >
      <div className="relative h-24 w-full overflow-hidden bg-gradient-to-br from-surface-2 via-surface to-canvas">
        <img
          src={imgPath(item.image_path)}
          alt=""
          className="h-full w-full object-cover"
          onClick={onView}
          onError={(e) => ((e.target as HTMLImageElement).style.display = 'none')}
        />
        {item.review_required && (
          <span className="absolute right-1.5 top-1.5 flex h-6 w-6 items-center justify-center rounded-full bg-danger-soft text-danger shadow-[0_0_10px_rgba(255,69,58,0.3)]">
            <span className="material-symbols-outlined text-[14px]">warning</span>
          </span>
        )}
      </div>
      <div className="px-2.5 py-2">
        <div className="truncate text-xs font-semibold text-ink">{item.camera_name}</div>
        <div className="font-mono text-[9px] tabular-nums text-ink-faint">
          {formatTimeShort(item.captured_at)}
        </div>
        {tags.length > 0 && <div className="mt-1.5 flex flex-wrap gap-1">{tags}</div>}
      </div>
    </div>
  );
}

function NavButton({
  label,
  icon,
  active = false,
  to,
}: {
  label: string;
  icon: string;
  active?: boolean;
  to?: string;
}) {
  const cls = `relative flex min-h-11 min-w-[64px] flex-1 flex-col items-center justify-center rounded-xl px-2 py-1 transition-all duration-100 active:scale-90 sm:px-4 ${
    active ? 'bg-accent text-canvas shadow-[0_0_14px_rgba(10,132,255,0.35)]' : 'text-ink-dim hover:text-accent-strong'
  }`;
  const inner = (
    <>
      <span className="material-symbols-outlined mb-0.5 text-[20px]">{icon}</span>
      <span className="whitespace-nowrap font-mono text-[10px] font-bold uppercase tracking-[0.08em]">{label}</span>
    </>
  );
  return to ? (
    <Link to={to} className={cls}>
      {inner}
    </Link>
  ) : (
    <button className={cls}>{inner}</button>
  );
}

function EmptyState({ icon, message }: { icon: string; message: string }) {
  return (
    <div className="flex animate-feed-in flex-col items-center gap-3 rounded-xl border border-edge bg-surface-high py-14 text-center">
      <span className="material-symbols-outlined text-3xl text-ink-faint">{icon}</span>
      <p className="text-xs uppercase tracking-[0.22em] text-ink-dim">{message}</p>
    </div>
  );
}
