import type { DetectedObject } from '../api/types';

const KNOWN_CLASSES = new Set([
  'person',
  'car',
  'train',
  'dog',
  'bicycle',
  'bird',
  'bench',
  'cat',
  'truck',
  'motorcycle',
  'skateboard',
  'sheep',
  'parking meter',
]);

export function parseDetections(json: string | null | undefined): DetectedObject[] {
  if (!json) return [];
  try {
    return JSON.parse(json) as DetectedObject[];
  } catch {
    return [];
  }
}

export function tagClass(cls: string): string {
  return KNOWN_CLASSES.has(cls) ? cls : 'other';
}

export function clsColor(cls: string): string {
  const map: Record<string, string> = {
    person: 'var(--color-warn)',
    car: 'var(--color-accent)',
    train: 'var(--color-danger)',
    dog: '#bf5af2',
    bicycle: '#64d2ff',
    bird: '#30d158',
    bench: '#ffd60a',
    motorcycle: '#ff375f',
    skateboard: '#ff9f0a',
    'parking meter': '#0a84ff',
    sheep: '#ffd60a',
    truck: '#64d2ff',
    cat: '#ff375f',
  };
  return map[cls] || 'var(--color-ink-faint)';
}

export function reasonClass(reason: string | null | undefined): string {
  if (!reason) return '';
  if (reason.startsWith('person_after_hours')) return 'person-after-hours';
  if (reason.startsWith('high_person_count')) return 'high-count';
  if (reason.startsWith('unexpected_objects')) return 'unexpected-objects';
  return '';
}

export function reasonLabel(reason: string | null | undefined): string {
  if (!reason) return 'Flagged';
  if (reason.startsWith('person_after_hours')) return 'Person After Hours';
  if (reason.startsWith('high_person_count')) return 'High Person Count';
  if (reason.startsWith('unexpected_objects')) return reason.replace('unexpected_objects:', '');
  return reason;
}
