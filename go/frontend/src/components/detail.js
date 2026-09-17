import { call } from '../api.js';
import { h, reportError, toast } from '../ui.js';
import {
  formatDate,
  formatDuration,
  formatPosition,
  formatSize,
  joinOrDash,
} from '../format.js';

const PROGRESS_SAVE_INTERVAL_MS = 10_000;

/**
 * 共享视频详情层：内嵌播放 / 外播 / 收藏 / 评分 / 截图灯箱。
 * opts: { libraryId, onChanged }
 */
export function createDetailLayer(state) {
  let overlay = null;
  let lightbox = null;
  let currentItem = null;
  let lastPlayed = false;

  function closeLightbox() {
    lightbox?.remove();
    lightbox = null;
  }

  function openLightbox(list, index) {
    let position = index;
    closeLightbox();
    const img = h('img', { id: 'lightbox-img', src: list[position], alt: '截图' });
    lightbox = h('div', {
      class: 'lightbox',
      onclick: (event) => { if (event.target === lightbox) closeLightbox(); },
    }, [
      h('button', {
        type: 'button', class: 'lb-nav lb-prev', text: '‹',
        onclick: () => {
          position = (position - 1 + list.length) % list.length;
          img.src = list[position];
        },
      }),
      img,
      h('button', {
        type: 'button', class: 'lb-nav lb-next', text: '›',
        onclick: () => {
          position = (position + 1) % list.length;
          img.src = list[position];
        },
      }),
    ]);
    document.body.append(lightbox);
  }

  function close() {
    if (!overlay) return;
    overlay.querySelectorAll('video').forEach((video) => {
      try {
        video.pause();
        video.removeAttribute('src');
        video.load();
      } catch {
        /* ignore */
      }
    });
    overlay.remove();
    overlay = null;
    currentItem = null;
    lastPlayed = false;
    closeLightbox();
  }

  function stars(value, onChange) {
    const root = h('div', { class: 'stars' });
    const paint = (n) => {
      root.replaceChildren();
      for (let i = 1; i <= 5; i += 1) {
        root.append(h('button', {
          type: 'button',
          class: i <= n ? 'on' : '',
          title: `${i} 星`,
          onclick: async () => {
            try {
              const next = i === n ? null : i;
              await call('SetRating', state.libraryId, currentItem.id, next);
              currentItem.rating = next;
              paint(next ?? 0);
              onChange?.(next);
            } catch (error) {
              reportError('保存评分失败', error);
            }
          },
        }, [h('span', { class: `ms${i <= n ? ' fill' : ''}`, text: 'star' })]));
      }
    };
    paint(value || 0);
    return root;
  }

  function startPlay(container, item) {
    const video = h('video', {
      src: item.videoUrl,
      controls: true,
      autoplay: true,
      playsinline: true,
    });

    const failHint = h('div', {
      class: 'play-fail hidden',
      style: 'display:none;margin-top:8px;padding:10px;border-radius:8px;background:var(--warn-bg);border:1px solid var(--warn-border);color:var(--warn);font-size:12px',
    }, [
      h('div', { text: '内嵌播放失败或无画面。HEVC / 10bit 建议改用外部播放器。' }),
      h('button', {
        type: 'button',
        class: 'btn secondary small',
        style: 'margin-top:6px',
        onclick: async () => {
          try {
            await call('OpenInPlayer', state.libraryId, item.id, true);
            toast('已交给外部播放器');
          } catch (error) {
            reportError('打开外部播放器失败', error);
          }
        },
        text: '用外部播放器打开',
      }),
    ]);

    const wrap = h('div', {}, [video, failHint]);
    container.replaceWith(wrap);

    let failed = false;
    video.addEventListener('error', () => {
      failed = true;
      failHint.style.display = 'block';
      toast('内嵌播放失败，请尝试外部播放器', 'warn');
    });

    if (item.watchPositionMs > 0) {
      video.addEventListener('loadedmetadata', () => {
        if (item.watchPositionMs / 1000 < video.duration - 5) {
          video.currentTime = item.watchPositionMs / 1000;
        }
      }, { once: true });
    }

    if (!lastPlayed) {
      lastPlayed = true;
      call('PlayEmbedded', state.libraryId, item.id).catch(() => {});
    }

    let lastSaved = 0;
    const save = (force) => {
      if (failed) return;
      const positionMs = Math.round(video.currentTime * 1000);
      if (!force && performance.now() - lastSaved < PROGRESS_SAVE_INTERVAL_MS) return;
      lastSaved = performance.now();
      call('SaveProgress', state.libraryId, item.id, positionMs).catch((error) => {
        console.error('保存播放进度失败', error);
      });
    };

    video.addEventListener('timeupdate', () => save(false));
    video.addEventListener('pause', () => save(true));
    video.addEventListener('ended', () => {
      save(true);
      call('SaveProgress', state.libraryId, item.id, 0).catch(() => {});
    });
  }

  function open(item) {
    close();
    currentItem = item;
    lastPlayed = false;

    const playerHost = item.playable && item.videoUrl
      ? h('div', {
        class: 'player',
        onclick: (event) => {
          if (event.target.tagName === 'VIDEO') return;
          startPlay(event.currentTarget, item);
        },
      }, [
        item.coverUrl
          ? h('img', {
            src: item.coverUrl,
            alt: `${item.fanha} 封面`,
            onerror: (e) => { e.target.style.visibility = 'hidden'; },
          })
          : h('div', {
            class: 'placeholder',
            style: 'aspect-ratio:16/9;display:flex;align-items:center;justify-content:center;color:var(--muted)',
          }, [h('span', { class: 'ms', text: 'play_circle' })]),
        h('div', { class: 'play-mask' }, [
          h('span', { class: 'ms', style: 'font-size:56px', text: 'play_arrow' }),
        ]),
      ])
      : h('img', {
        class: 'detail-cover',
        src: item.coverUrl || '',
        alt: `${item.fanha} 封面`,
        onerror: (e) => { e.target.style.visibility = 'hidden'; },
      });

    const resumeHint = item.watchPositionMs > 0
      ? `（上次 ${formatPosition(item.watchPositionMs)}）`
      : '';

    const actions = [
      h('button', {
        type: 'button',
        class: `btn ${item.favorite ? 'primary' : 'secondary'}`,
        onclick: async (event) => {
          try {
            const favorite = await call('ToggleFavorite', state.libraryId, item.id);
            item.favorite = favorite;
            event.currentTarget.className = `btn ${favorite ? 'primary' : 'secondary'}`;
            event.currentTarget.replaceChildren(
              h('span', { class: `ms${favorite ? ' fill' : ''}`, text: 'favorite' }),
              h('span', { text: favorite ? '已收藏' : '收藏' }),
            );
            // 详情按钮 label 更新后，主列表由调用方刷新
            item.onFavoriteChange?.(favorite);
          } catch (error) {
            reportError('切换收藏失败', error);
          }
        },
      }, [
        h('span', { class: `ms${item.favorite ? ' fill' : ''}`, text: 'favorite' }),
        h('span', { text: item.favorite ? '已收藏' : '收藏' }),
      ]),
      h('button', {
        type: 'button',
        class: 'btn secondary',
        onclick: async () => {
          try {
            await call('OpenInPlayer', state.libraryId, item.id, true);
            toast('已交给外部播放器');
          } catch (error) {
            reportError('打开外部播放器失败', error);
          }
        },
      }, [
        h('span', { class: 'ms', text: 'open_in_new' }),
        h('span', { text: `外部播放器打开${resumeHint}` }),
      ]),
      item.watchPositionMs > 0
        ? h('button', {
          type: 'button',
          class: 'btn ghost',
          onclick: async () => {
            try {
              await call('ClearProgress', state.libraryId, item.id);
              item.watchPositionMs = 0;
              toast('已清除播放进度');
              await open(item);
            } catch (error) {
              reportError('清除进度失败', error);
            }
          },
        }, [h('span', { class: 'ms', text: 'restart_alt' }), h('span', { text: '从头开始' })])
        : null,
    ].filter(Boolean);

    const ratingRow = h('div', {
      class: 'detail-row',
      style: 'display:flex;align-items:center;gap:10px;padding:8px 0',
    }, [
      h('b', { text: '评分' }),
      stars(item.rating, () => {
        item.onRatingChange?.(item.rating);
      }),
      item.rating
        ? h('span', { class: 'mono', style: 'font-size:12px;color:var(--muted)', text: `${item.rating} / 5` })
        : h('span', { style: 'font-size:12px;color:var(--faint)', text: '未评分' }),
    ]);

    const rows = [
      ['番号', item.fanha],
      ['演员', item.actress || joinOrDash(item.cast)],
      ['发布时间', formatDate(item.releaseDate)],
      ['文件真实时长', formatDuration(item.durationMs)],
      ['站点标注时长', item.siteLengthMin ? `${item.siteLengthMin} 分钟` : '—'],
      ['分辨率', item.width > 0 ? `${item.width}×${item.height}` : '—'],
      ['编码', [item.vcodec, item.acodec].filter(Boolean).join(' / ') || '—'],
      ['类别', joinOrDash(item.genres)],
      ['文件名', item.stem || '—'],
      ['文件大小', formatSize(item.fileSize)],
      ['刮削时间', item.scrapedAt || '—'],
      ['播放次数', item.playCount > 0 ? String(item.playCount) : '—'],
    ].map(([label, value]) => h('div', { class: 'detail-row' }, [
      `${label}：`,
      h('b', { text: String(value) }),
    ]));

    const shots = (item.shotUrls ?? []).map((url, index) => h('img', {
      src: url,
      alt: `截图 ${index + 1}`,
      loading: 'lazy',
      onerror: (e) => { e.target.style.visibility = 'hidden'; },
      onclick: () => openLightbox(item.shotUrls, index),
    }));

    const inner = h('div', { class: 'overlay-inner' }, [
      playerHost,
      h('div', { class: 'detail-title', text: item.title || item.fanha }),
      ratingRow,
      h('div', { class: 'detail-actions' }, actions),
      item.missing
        ? h('div', { class: 'detail-row' }, [
          h('span', { class: 'badge broken', text: '文件已不在磁盘上' }),
          ' 索引里仍有记录。重新扫描后该条目会消失。',
        ])
        : null,
      !item.scraped
        ? h('div', { class: 'detail-row' }, [
          h('span', { class: 'badge pending', text: '未刮削' }),
          ' 元数据可能不完整，可在「扫描与待处理」中刮削。',
        ])
        : null,
      h('div', { class: 'detail-rows' }, rows),
      shots.length
        ? h('div', {}, [
          h('div', { class: 'panel-title', style: 'margin-top:16px', text: `剧照（${shots.length}）` }),
          h('div', { class: 'shots' }, shots),
        ])
        : h('div', { class: 'empty-desc', style: 'margin-top:12px', text: '暂无剧照。完成刮削后会显示。' }),
    ]);

    overlay = h('div', {
      class: 'overlay',
      onclick: (event) => { if (event.target === overlay) close(); },
    }, [
      h('button', {
        type: 'button',
        class: 'overlay-close',
        onclick: close,
      }, [h('span', { class: 'ms', text: 'close' })]),
      inner,
    ]);

    document.body.append(overlay);
  }

  const onKey = (event) => {
    if (event.key !== 'Escape') return;
    if (lightbox) closeLightbox();
    else if (overlay) close();
  };
  document.addEventListener('keydown', onKey);

  return { open, close, isOpen: () => Boolean(overlay) };
}
