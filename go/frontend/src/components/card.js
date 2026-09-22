import { h } from '../ui.js';
import { formatDuration, formatPosition, formatRelativeTime, formatTimecode } from '../format.js';

/** 状态点/文字：未刮削 · 已刮削 · 缺图 · 文件异常 */
export function scrapeState(item) {
  if (item.missing || item.playable === false) return 'broken';
  if (!item.scraped) return 'pending';
  if (!item.coverUrl && !item.coverRel) return 'missing-art';
  return 'scraped';
}

const STATE_LABEL = {
  pending: '未刮削',
  scraped: '已刮削',
  'missing-art': '缺图',
  broken: '文件异常',
};

/**
 * 2:3 海报卡片。
 * opts: { onOpen, onPlay, onToggleFavorite }
 */
export function videoCard(item, opts = {}) {
  const state = scrapeState(item);
  const progress =
    item.durationMs > 0 && item.watchPositionMs > 0
      ? Math.min(100, Math.round((item.watchPositionMs / item.durationMs) * 100))
      : 0;

  const poster = h('div', { class: 'poster' }, [
    item.coverUrl
      ? h('img', {
        src: item.coverUrl,
        alt: `${item.fanha} 封面`,
        loading: 'lazy',
        onerror: (event) => { event.target.style.visibility = 'hidden'; },
      })
      : h('div', { class: 'placeholder' }, [
        h('span', { class: 'ms', text: 'movie_filter' }),
        h('span', { class: 'mono', style: 'font-size:10px', text: 'NO ART' }),
      ]),
    h('span', { class: 'fanha-chip mono', text: item.fanha }),
    item.favorite
      ? h('span', { class: 'fav-btn', title: '已收藏' }, [h('span', { class: 'ms fill', text: 'favorite' })])
      : null,
    h('div', { class: 'poster-meta' }, [
      h('span', { class: 'stat' }, [
        h('span', {
          class: 'dot',
          style: `width:6px;height:6px;border-radius:50%;background:var(--${
            state === 'scraped' ? 'ok' : state === 'missing-art' ? 'warn' : state === 'broken' ? 'danger' : 'muted'
          })`,
        }),
        h('span', { text: STATE_LABEL[state] }),
      ]),
      h('span', { class: 'stat' }, [
        h('span', { text: item.durationMs > 0 ? formatDuration(item.durationMs) : '—' }),
        item.fileSize > 0 ? h('span', { text: ` · ${(item.fileSize / 1024 / 1024 / 1024).toFixed(2)} GB` }) : null,
      ]),
    ]),
    progress > 0
      ? h('div', {
        style: `position:absolute;left:0;right:0;bottom:0;height:3px;background:rgba(0,0,0,.5);z-index:3`,
      }, [
        h('div', {
          style: `height:100%;width:${progress}%;background:var(--primary)`,
        }),
      ])
      : null,
  ]);

  const card = h('button', {
    type: 'button',
    class: 'card',
    title: `${item.fanha} · 单击详情，双击播放`,
    onclick: (event) => {
      // event.detail === 2 表示双击的第二次；直接播放。
      if (event.detail >= 2) opts.onPlay?.(item);
      else opts.onOpen?.(item);
    },
  }, [
    poster,
    h('div', { class: 'card-body' }, [
      h('div', { class: 'card-actress', text: item.actress || '—' }),
      h('div', { class: 'card-title', text: item.title || '（无标题）' }),
      h('div', { class: 'card-foot' }, [
        h('span', { text: item.releaseDate || '—' }),
        h('span', { text: item.rating ? `★ ${item.rating}` : '' }),
      ]),
    ]),
  ]);

  return card;
}

/**
 * 继续观看：横排大卡片（16:9 缩略图 + 底部进度条，点击续播）。
 * opts: { onPlay, onClear }
 */
export function continueCard(item, opts = {}) {
  const progress =
    item.durationMs > 0 && item.watchPositionMs > 0
      ? Math.min(100, Math.round((item.watchPositionMs / item.durationMs) * 100))
      : 0;

  return h('div', { class: 'continue-card' }, [
    h('button', {
      type: 'button',
      class: 'continue-thumb',
      title: `${item.fanha} · 继续播放`,
      onclick: () => opts.onPlay?.(item),
    }, [
      item.coverUrl
        ? h('img', {
          src: item.coverUrl,
          alt: item.fanha,
          loading: 'lazy',
          onerror: (e) => { e.target.style.visibility = 'hidden'; },
        })
        : h('div', { class: 'placeholder' }, [h('span', { class: 'ms', text: 'movie' })]),
      h('span', { class: 'fanha-chip mono', text: item.fanha }),
      h('span', { class: 'continue-play', 'aria-hidden': 'true' }, [
        h('span', { class: 'ms', text: 'play_arrow' }),
      ]),
      h('div', { class: 'continue-bar' }, [h('div', { style: `width:${progress}%` })]),
    ]),
    h('div', { class: 'continue-body' }, [
      h('div', { class: 'continue-title', text: item.title || '（无标题）' }),
      h('div', { class: 'continue-meta mono' }, [
        h('span', { text: `${formatTimecode(item.watchPositionMs)} / ${formatTimecode(item.durationMs)}` }),
        h('span', { class: 'continue-pct', text: `${progress}%` }),
      ]),
      h('div', { class: 'continue-foot' }, [
        h('span', { class: 'muted', text: item.actress || '—' }),
        h('span', { class: 'muted', text: item.lastPlayedAt ? formatRelativeTime(item.lastPlayedAt) : '' }),
        opts.onClear
          ? h('button', {
            type: 'button',
            class: 'btn ghost small continue-clear',
            title: '清除进度',
            onclick: () => opts.onClear?.(item),
          }, [h('span', { class: 'ms', text: 'clear_all' }), h('span', { text: '清除' })])
          : null,
      ]),
    ]),
  ]);
}

/** 历史列表行卡片（横幅） */
export function historyCard(item, opts = {}) {
  const progress =
    item.durationMs > 0 && item.watchPositionMs > 0
      ? Math.min(100, Math.round((item.watchPositionMs / item.durationMs) * 100))
      : 0;

  return h('div', { class: 'history-card' }, [
    h('button', {
      type: 'button',
      class: 'history-poster',
      onclick: () => opts.onOpen?.(item),
    }, [
      item.coverUrl
        ? h('img', {
          src: item.coverUrl,
          alt: item.fanha,
          loading: 'lazy',
          onerror: (e) => { e.target.style.visibility = 'hidden'; },
        })
        : h('div', { class: 'placeholder' }, [h('span', { class: 'ms', text: 'movie' })]),
    ]),
    h('div', { class: 'history-body' }, [
      h('div', { class: 'history-top' }, [
        h('span', { class: 'mono', style: 'color:var(--primary);font-weight:600', text: item.fanha }),
        h('span', { class: 'muted', text: item.actress }),
      ]),
      h('div', { class: 'history-title', text: item.title || '（无标题）' }),
      h('div', { class: 'bar', style: 'margin:8px 0' }, [
        h('div', { style: `width:${progress}%` }),
      ]),
      h('div', { class: 'history-meta mono' }, [
        h('span', {
          text: item.watchPositionMs > 0
            ? `上次 ${formatPosition(item.watchPositionMs)} / ${formatDuration(item.durationMs)}`
            : formatDuration(item.durationMs),
        }),
        item.lastPlayedAt
          ? h('span', { text: item.lastPlayedAt.replace('T', ' ').slice(0, 16) })
          : null,
        item.playCount > 0 ? h('span', { text: `×${item.playCount}` }) : null,
      ]),
    ]),
    h('div', { class: 'history-actions' }, [
      h('button', {
        type: 'button',
        class: 'btn primary small',
        onclick: () => opts.onPlay?.(item),
      }, [h('span', { class: 'ms', text: 'play_arrow' }), h('span', { text: '继续' })]),
      opts.onClear
        ? h('button', {
          type: 'button',
          class: 'btn ghost small',
          onclick: () => opts.onClear?.(item),
        }, [h('span', { class: 'ms', text: 'clear_all' }), h('span', { text: '清除' })])
        : null,
    ]),
  ]);
}
