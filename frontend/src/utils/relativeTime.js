const MINUTE = 60;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;
const WEEK = 7 * DAY;
const MONTH = 30 * DAY;
const YEAR = 365 * DAY;

export function formatRelative(input) {
  if (!input) return '';
  const then = new Date(input).getTime();
  if (Number.isNaN(then)) return '';
  const diffSec = Math.max(0, Math.floor((Date.now() - then) / 1000));

  if (diffSec < 45) return 'just now';
  if (diffSec < 90) return '1 minute ago';
  if (diffSec < HOUR) return `${Math.round(diffSec / MINUTE)} minutes ago`;
  if (diffSec < 90 * MINUTE) return '1 hour ago';
  if (diffSec < DAY) return `${Math.round(diffSec / HOUR)} hours ago`;
  if (diffSec < 2 * DAY) return 'yesterday';
  if (diffSec < WEEK) return `${Math.round(diffSec / DAY)} days ago`;
  if (diffSec < 2 * WEEK) return '1 week ago';
  if (diffSec < MONTH) return `${Math.round(diffSec / WEEK)} weeks ago`;
  if (diffSec < 2 * MONTH) return '1 month ago';
  if (diffSec < YEAR) return `${Math.round(diffSec / MONTH)} months ago`;
  if (diffSec < 2 * YEAR) return '1 year ago';
  return `${Math.round(diffSec / YEAR)} years ago`;
}
