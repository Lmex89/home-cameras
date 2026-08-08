import { useCallback, useEffect, useRef, useState } from 'react';
import { imgPath } from '../api/client';

export interface LightboxItem {
  image_path: string;
  label: string;
}

interface LightboxProps {
  items: LightboxItem[];
  index: number;
  onClose: () => void;
  onIndexChange: (index: number) => void;
}

export function Lightbox({ items, index, onClose, onIndexChange }: LightboxProps) {
  const [dragging, setDragging] = useState(false);
  const [offset, setOffset] = useState(0);
  const touch = useRef({ startX: 0, lastX: 0, swiping: false });

  const item = items[index];

  const prev = useCallback(() => {
    if (index > 0) onIndexChange(index - 1);
  }, [index, onIndexChange]);

  const next = useCallback(() => {
    if (index < items.length - 1) onIndexChange(index + 1);
  }, [index, items.length, onIndexChange]);

  useEffect(() => {
    if (items.length === 0) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
      if (e.key === 'ArrowLeft') prev();
      if (e.key === 'ArrowRight') next();
    };
    window.addEventListener('keydown', onKey);
    document.body.style.overflow = 'hidden';
    return () => {
      window.removeEventListener('keydown', onKey);
      document.body.style.overflow = '';
    };
  }, [items.length, onClose, prev, next]);

  useEffect(() => {
    setOffset(0);
    setDragging(false);
    for (const delta of [1, -1, 2, -2]) {
      const idx = index + delta;
      if (idx >= 0 && idx < items.length) {
        const img = new Image();
        img.src = imgPath(items[idx].image_path);
      }
    }
  }, [index, items]);

  const onPointerDown = (e: React.PointerEvent) => {
    if (e.target instanceof Element && e.target.closest('.lb-close, .lb-nav')) return;
    touch.current = { startX: e.clientX, lastX: e.clientX, swiping: false };
    setDragging(true);
  };

  const onPointerMove = (e: React.PointerEvent) => {
    if (!dragging) return;
    const dx = e.clientX - touch.current.startX;
    touch.current.lastX = e.clientX;
    if (!touch.current.swiping && Math.abs(dx) >= 8) touch.current.swiping = true;
    const atEdge = (index === 0 && dx > 0) || (index >= items.length - 1 && dx < 0);
    setOffset(atEdge ? dx * 0.15 : dx);
  };

  const onPointerUp = () => {
    if (!dragging) return;
    const dx = touch.current.lastX - touch.current.startX;
    if (touch.current.swiping) {
      if (dx < -50) next();
      else if (dx > 50) prev();
    }
    setDragging(false);
    setOffset(0);
  };

  if (items.length === 0 || !item) return null;

  return (
    <div className="lb open" onPointerDown={onPointerDown} onPointerMove={onPointerMove} onPointerUp={onPointerUp} onPointerLeave={onPointerUp}>
      <button
        className="lb-close"
        onClick={onClose}
        onPointerDown={(e) => e.stopPropagation()}
      >
        ✕
      </button>
      <div className={`lb-track${dragging ? ' dragging' : ''}`} style={{ transform: offset ? `translateX(${offset}px)` : undefined }}>
        <img src={imgPath(item.image_path)} alt={item.label} />
      </div>
      <div className="lb-info">{item.label}</div>
      {items.length > 1 && index > 0 && (
        <button className="lb-nav lb-prev" onClick={prev} onPointerDown={(e) => e.stopPropagation()}>
          ‹
        </button>
      )}
      {items.length > 1 && index < items.length - 1 && (
        <button className="lb-nav lb-next" onClick={next} onPointerDown={(e) => e.stopPropagation()}>
          ›
        </button>
      )}
    </div>
  );
}
