import { call } from '../api.js';
import { clear, h, reportError, toast } from '../ui.js';
import { emptyState, pageHeader } from '../components/shell.js';
import { continueCard, historyCard } from '../components/card.js';
import { createDetailLayer } from '../components/detail.js';

/** kind: 'continue' | 'recent' */
export function createHistoryView(state, kind) {
  const title = kind === 'continue' ? '继续观看' : '最近播放';
  const iconName = kind === 'continue' ? 'play_circle' : 'history';

  const list = h('div', { class: kind === 'continue' ? 'continue-row' : 'history-grid' });
  const headerHost = h('div');
  const root = h('div', { class: 'page', dataset: { view: kind } }, [headerHost, h('div', { class: 'page-body' }, [list])]);
  const detail = createDetailLayer(state);

  const view = { items: [], loading: false };

  function updateHeader() {
    clear(headerHost);
    headerHost.append(pageHeader({
      title,
      sub: kind === 'continue'
        ? '有播放进度且尚未看完'
        : '按最近播放时间倒序',
      actions: [
        kind === 'recent'
          ? h('button', {
            type: 'button',
            class: 'btn danger small',
            onclick: async () => {
              if (!state.libraryId) return;
              if (!window.confirm('清空该库的最近播放记录？收藏与评分不受影响。')) return;
              try {
                await call('ClearRecentHistory', state.libraryId);
                toast('已清空最近播放');
                await load();
              } catch (error) {
                reportError('清空失败', error);
              }
            },
          }, [h('span', { class: 'ms', text: 'delete_sweep' }), h('span', { text: '清空记录' })])
          : null,
        h('button', {
          type: 'button',
          class: 'btn secondary small',
          onclick: () => load().catch((e) => reportError('刷新失败', e)),
        }, [h('span', { class: 'ms', text: 'refresh' }), h('span', { text: '刷新' })]),
      ].filter(Boolean),
    }));
  }

  async function load() {
    updateHeader();
    clear(list);
    if (!state.libraryId) {
      list.append(emptyState({
        icon: 'video_library',
        title: '还没有视频库',
        desc: '先添加视频库并播放几集，这里就会出现记录。',
      }));
      return;
    }

    view.loading = true;
    try {
      view.items = kind === 'continue'
        ? (await call('ListContinueWatching', state.libraryId)) ?? []
        : (await call('ListRecentPlays', state.libraryId)) ?? [];
    } catch (error) {
      reportError('读取记录失败', error);
      view.items = [];
    } finally {
      view.loading = false;
    }

    if (!view.items.length) {
      list.append(emptyState({
        icon: iconName,
        title: kind === 'continue' ? '没有未看完的视频' : '还没有播放记录',
        desc: kind === 'continue'
          ? '从「演员」或「全部影片」点开封面播放，进度会自动记录在这里。'
          : '播放任意视频后，这里会按时间倒序显示。',
      }));
      return;
    }

    const render = kind === 'continue' ? continueCard : historyCard;
    const clearProgress = kind === 'continue'
      ? async (it) => {
        try {
          await call('ClearProgress', state.libraryId, it.id);
          toast('已清除进度');
          await load();
        } catch (error) {
          reportError('清除进度失败', error);
        }
      }
      : null;

    for (const item of view.items) {
      list.append(render(item, {
        onOpen: (it) => detail.open(it),
        onPlay: (it) => {
          detail.open(it);
          setTimeout(() => {
            document.querySelector('.overlay .player')?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
          }, 50);
        },
        onClear: clearProgress,
      }));
    }
  }

  return {
    el: root,
    async onActivate() {
      await load();
    },
    async onLibraryChange() {
      detail.close();
      await load();
    },
  };
}
