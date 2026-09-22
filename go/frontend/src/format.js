// 展示用格式化。

const SIZE_UNITS = ['B', 'KB', 'MB', 'GB', 'TB'];

export function formatSize(bytes) {
  if (!bytes || bytes <= 0) return '-';
  let value = bytes;
  let unit = 0;
  while (value >= 1024 && unit < SIZE_UNITS.length - 1) {
    value /= 1024;
    unit += 1;
  }
  return `${value.toFixed(value >= 100 || unit === 0 ? 0 : 1)} ${SIZE_UNITS[unit]}`;
}

/** 真实时长（毫秒）。与站点标注时长不同源，见 index.Row 的说明。 */
export function formatDuration(ms) {
  if (!ms || ms <= 0) return '-';
  const total = Math.round(ms / 1000);
  const hours = Math.floor(total / 3600);
  const minutes = Math.floor((total % 3600) / 60);
  if (hours > 0) return `${hours} 小时 ${minutes} 分`;
  return `${minutes} 分`;
}

export function formatDate(value) {
  return value || '-';
}

export function joinOrDash(list) {
  return Array.isArray(list) && list.length ? list.join('、') : '-';
}

/** 把毫秒位置格式化为 mm:ss，用于续播提示。 */
export function formatPosition(ms) {
  if (!ms || ms <= 0) return '';
  const total = Math.round(ms / 1000);
  const minutes = Math.floor(total / 60);
  const seconds = total % 60;
  return `${minutes}:${String(seconds).padStart(2, '0')}`;
}

/** 毫秒 → h:mm:ss（不足 1 小时时 m:ss），用于继续观看卡片进度时刻。 */
export function formatTimecode(ms) {
  if (!ms || ms <= 0) return '0:00';
  const total = Math.floor(ms / 1000);
  const hours = Math.floor(total / 3600);
  const minutes = Math.floor((total % 3600) / 60);
  const seconds = total % 60;
  const mm = String(minutes).padStart(2, '0');
  const ss = String(seconds).padStart(2, '0');
  return hours > 0 ? `${hours}:${mm}:${ss}` : `${minutes}:${ss}`;
}

/** 相对时间：刚刚 / N分钟前 / N小时前 / 昨天 / N天前 / 短日期。 */
export function formatRelativeTime(value) {
  if (!value) return '';
  const time = new Date(String(value).replace(' ', 'T')).getTime();
  if (Number.isNaN(time)) return String(value).replace('T', ' ').slice(0, 16);
  const diff = Date.now() - time;
  if (diff < 60_000) return '刚刚';
  const minutes = Math.floor(diff / 60_000);
  if (minutes < 60) return `${minutes}分钟前`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}小时前`;
  const days = Math.floor(hours / 24);
  if (days === 1) return '昨天';
  if (days < 7) return `${days}天前`;
  return String(value).replace('T', ' ').slice(0, 10);
}
