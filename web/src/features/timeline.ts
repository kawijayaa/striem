import type { EventRow } from '../types';

export interface TimelineController {
  render(rows: EventRow[]): void;
}

export interface TimelineElements {
  root: HTMLElement;
  bars: HTMLElement;
  start: HTMLElement;
  end: HTMLElement;
}

function eventTime(row: EventRow): number | undefined {
  const value = row.TimeGenerated;
  if (typeof value !== 'string' && typeof value !== 'number') return undefined;
  const timestamp = new Date(value).getTime();
  return Number.isNaN(timestamp) ? undefined : timestamp;
}

export function createTimeline(
  elements: TimelineElements,
  onSelect: (start: Date, end: Date) => void,
): TimelineController {
  function render(rows: EventRow[]): void {
    const points = rows.map(eventTime).filter((value): value is number => value !== undefined);
    elements.root.classList.toggle('hidden', points.length === 0);
    if (points.length === 0) return;

    const minimum = Math.min(...points);
    const maximum = Math.max(...points);
    const availableBuckets = window.innerWidth <= 600
      ? Math.max(1, Math.floor((window.innerWidth - 50) / 28))
      : 24;
    const bucketCount = Math.min(availableBuckets, Math.max(1, Math.ceil(Math.sqrt(points.length))));
    const width = Math.max(1, maximum - minimum + 1);
    const buckets = Array<number>(bucketCount).fill(0);
    points.forEach(value => {
      const index = Math.min(bucketCount - 1, Math.floor(((value - minimum) / width) * bucketCount));
      buckets[index] = (buckets[index] ?? 0) + 1;
    });
    const peak = Math.max(...buckets);

    elements.bars.replaceChildren();
    buckets.forEach((count, index) => {
      const start = new Date(minimum + (width * index) / bucketCount);
      const end = new Date(minimum + (width * (index + 1)) / bucketCount);
      const bar = document.createElement('button');
      bar.type = 'button';
      bar.className = 'timeline-bar';
      const fill = document.createElement('span');
      fill.className = 'timeline-bar-fill';
      fill.style.setProperty('--bar', String(index));
      fill.style.height = count ? `${Math.max(4, (count / peak) * 100)}%` : '0';
      const label = `${count} event${count === 1 ? '' : 's'} from ${start.toLocaleString()} to ${end.toLocaleString()}`;
      bar.title = `${label}. Add this time range to the query.`;
      bar.setAttribute('aria-label', bar.title);
      bar.disabled = count === 0;
      bar.addEventListener('click', () => onSelect(start, end));
      bar.append(fill);
      elements.bars.append(bar);
    });

    elements.start.textContent = new Date(minimum).toLocaleString();
    elements.end.textContent = new Date(maximum).toLocaleString();
  }

  return { render };
}
