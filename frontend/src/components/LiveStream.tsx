import { useState, useRef, useEffect, useCallback } from 'react';

export interface LiveStreamProps {
  cameraId: number;
  className?: string;
}

/**
 * LiveStream renders a live MJPEG stream from a camera.
 * Shows a play button initially, then swaps to the stream.
 * Automatically reconnects on failure with exponential backoff.
 */
export function LiveStream({ cameraId, className = '' }: LiveStreamProps) {
  const [status, setStatus] = useState<'idle' | 'loading' | 'live' | 'error'>('idle');
  const reconnectRef = useRef(0);
  const imgRef = useRef<HTMLImageElement | null>(null);
  const keyRef = useRef(0);

  const streamUrl = `/api/cameras/${cameraId}/stream`;

  const start = useCallback(() => {
    setStatus('loading');
    reconnectRef.current = 0;
    keyRef.current += 1;
  }, []);

  const stop = useCallback(() => {
    setStatus('idle');
    reconnectRef.current = 0;
    keyRef.current += 1;
  }, []);

  useEffect(() => {
    if (status !== 'loading') return;

    const img = imgRef.current;
    if (!img) return;

    const timeout = setTimeout(() => {
      if (imgRef.current) {
        imgRef.current.onload = () => {
          setStatus('live');
          reconnectRef.current = 0;
        };
        imgRef.current.onerror = () => {
          const attempt = reconnectRef.current;
          reconnectRef.current = attempt + 1;
          setStatus('error');

          const delay = Math.min(1000 * 2 ** attempt, 5000);
          setTimeout(() => {
            keyRef.current += 1;
            setStatus('loading');
          }, delay);
        };
      }
    }, 50);

    return () => {
      clearTimeout(timeout);
      if (imgRef.current) {
        imgRef.current.onload = null;
        imgRef.current.onerror = null;
      }
    };
  }, [status, keyRef.current]);

  const toggle = status === 'idle' || status === 'error' ? start : stop;

  if (status === 'idle' || status === 'error') {
    return (
      <button
        onClick={toggle}
        className={`flex items-center justify-center rounded-lg border border-edge-soft bg-glass/50 backdrop-blur-sm transition-all hover:border-accent hover:text-accent-strong active:scale-95 ${className}`}
      >
        <span className="material-symbols-outlined text-2xl">
          {status === 'error' ? 'replay' : 'play_arrow'}
        </span>
        <span className="ml-2 text-xs font-bold uppercase tracking-wider text-ink-dim">
          {status === 'error' ? 'Reconnect' : 'Live'}
        </span>
      </button>
    );
  }

  return (
    <div className={`relative rounded-lg overflow-hidden border border-edge-soft bg-black ${className}`}>
      {status === 'loading' && (
        <div className="absolute inset-0 flex items-center justify-center z-10 pointer-events-none">
          <div className="animate-spin h-6 w-6 border-2 border-accent border-t-transparent rounded-full" />
        </div>
      )}
      <img
        key={keyRef.current}
        ref={imgRef}
        src={`${streamUrl}?_=${keyRef.current}`}
        alt="Live camera stream"
        className="w-full h-full object-cover"
      />
      {status === 'live' && (
        <div className="absolute top-2 right-2 flex items-center gap-2 z-10">
          <span className="inline-flex items-center gap-1 rounded bg-red-600/80 px-2 py-0.5 text-[10px] font-bold uppercase tracking-wider text-white">
            <span className="inline-block h-1.5 w-1.5 rounded-full bg-white animate-pulse" />
            Live
          </span>
          <button
            onClick={stop}
            className="rounded bg-black/50 p-1 text-white hover:bg-black/70 transition-colors"
            aria-label="Stop live stream"
          >
            <span className="material-symbols-outlined text-sm">stop</span>
          </button>
        </div>
      )}
    </div>
  );
}
