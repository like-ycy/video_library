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
