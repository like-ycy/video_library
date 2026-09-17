import { call } from '../api.js';
import { clear, h, reportError, setBusy, toast } from '../ui.js';
import { emptyState, pageHeader } from '../components/shell.js';
import { applyTheme } from '../theme.js';
import { setScraperStatus } from '../ui.js';
import { resetOnboarding } from './Onboarding.js';

/**
 * 设置模块多页：library / scraper / player / env / appearance / about
 */
export function createSettingsView(state, section) {
  const headerHost = h('div');
  const body = h('div', { class: 'page-body settings-page' });
  const root = h('div', { class: 'page', dataset: { view: `settings-${section}` } }, [headerHost, body]);

  const TITLES = {
    library: ['视频库管理', '添加 / 移除库，查看可访问状态，重建索引'],
    scraper: ['刮削器设置', 'scraper.exe 路径、并发、超时与自检'],
    player: ['播放器设置', '外部播放器与内嵌失败兜底'],
    env: ['环境诊断', 'Chrome、驱动、ffprobe、协议兼容性'],
    appearance: ['外观与配置文件', '主题与配置文件位置'],
    about: ['关于与更新', '版本信息（更新能力第三期）'],
  };

  function updateHeader() {
    clear(headerHost);
    const [title, sub] = TITLES[section] || ['设置', ''];
    headerHost.append(pageHeader({ title, sub }));
  }

  function fieldRow(labelText, control, hint) {
    return h('div', { class: 'form-row' }, [
      h('div', { class: 'form-label' }, [
        h('label', { text: labelText }),
        hint ? h('div', { class: 'panel-desc', text: hint }) : null,
      ]),
      h('div', { class: 'form-control' }, [control]),
    ]);
  }

  // ── 视频库管理 ─────────────────────────────────────────────────────────
  async function renderLibrary() {
    updateHeader();
    clear(body);

    const listHost = h('div', { class: 'panel' });
    body.append(h('div', { style: 'display:flex;gap:8px;margin-bottom:14px' }, [
      h('button', {
        type: 'button',
        class: 'btn primary',
        onclick: async () => {
          setBusy(true);
          try {
            const rootPath = await call('PickLibraryRoot');
            if (!rootPath) return;
            const lib = await call('AddLibrary', rootPath);
            toast(`已添加：${lib.root}`, 'ok');
            await renderLibrary();
            window.dispatchEvent(new CustomEvent('cinevault:libraries-changed'));
          } catch (error) {
            reportError('添加失败', error);
          } finally {
            setBusy(false);
          }
        },
      }, [h('span', { class: 'ms', text: 'add' }), h('span', { text: '添加视频库' })]),
    ]));

    const libs = (await call('ListLibraries')) ?? [];
    if (!libs.length) {
      listHost.append(emptyState({
        icon: 'video_library',
        title: '尚未添加视频库',
        desc: '目录结构：<库根>/<演员>/<视频文件>.mp4',
        hint: 'D:\\Videos\\Library',
      }));
      body.append(listHost);
      return;
    }

    listHost.append(h('div', { class: 'panel-title', text: `已配置（${libs.length}）` }));
    for (const lib of libs) {
      listHost.append(h('div', { class: 'lib-row' }, [
        h('div', { class: 'lib-main' }, [
          h('div', { style: 'display:flex;align-items:center;gap:8px' }, [
            h('span', {
              class: `badge ${lib.available ? 'scraped' : 'broken'}`,
              text: lib.available ? '可访问' : '不可访问',
            }),
            h('span', { class: 'mono', text: lib.root }),
          ]),
          h('div', { class: 'panel-desc', text: `ID: ${lib.id}` }),
        ]),
        h('div', { style: 'display:flex;gap:6px;flex-wrap:wrap' }, [
          h('button', {
            type: 'button',
            class: 'btn secondary small',
            disabled: !lib.available,
            onclick: async () => {
              if (state.libraryId !== lib.id) {
                window.dispatchEvent(new CustomEvent('cinevault:select-library', { detail: lib.id }));
              }
              setBusy(true);
              try {
                await call('ImportLibrary', lib.id);
                toast('已更新索引', 'ok');
              } catch (error) {
                reportError('导入失败', error);
              } finally {
                setBusy(false);
              }
            },
          }, [h('span', { class: 'ms', text: 'sync' }), h('span', { text: '更新索引' })]),
          h('button', {
            type: 'button',
            class: 'btn secondary small',
            disabled: !lib.available,
            onclick: async () => {
              if (!window.confirm('重建索引会清空该库的本地索引库后重扫。收藏/评分/进度按业务键保留。继续？')) return;
              setBusy(true);
              try {
                await call('RebuildIndex', lib.id);
                toast('重建完成', 'ok');
              } catch (error) {
                reportError('重建失败', error);
              } finally {
                setBusy(false);
              }
            },
          }, [h('span', { class: 'ms', text: 'restart_alt' }), h('span', { text: '重建索引' })]),
          h('button', {
            type: 'button',
            class: 'btn danger small',
            onclick: async () => {
              if (!window.confirm('移出视频库不会删除磁盘文件。仅从配置移除。继续？')) return;
              try {
                await call('RemoveLibrary', lib.id);
                toast('已移出');
                if (state.libraryId === lib.id) {
                  window.dispatchEvent(new CustomEvent('cinevault:libraries-changed'));
                }
                await renderLibrary();
              } catch (error) {
                reportError('移除失败', error);
              }
            },
          }, [h('span', { class: 'ms', text: 'delete' }), h('span', { text: '移出' })]),
        ]),
      ]));
    }

    body.append(listHost);
    body.append(h('div', { class: 'panel', style: 'margin-top:14px' }, [
      h('div', { class: 'panel-title', text: '说明' }),
      h('div', { class: 'panel-desc', text: '移除视频库不会删除磁盘上的任何文件。索引数据库保留在用户配置目录，重新添加同一根目录即可恢复浏览。' }),
    ]));
  }

  // ── 刮削器设置 ─────────────────────────────────────────────────────────
  async function renderScraper() {
    updateHeader();
    clear(body);
    const cfg = await call('GetConfig');

    const scraperPath = h('input', { class: 'field', value: cfg.scraperPath || '', placeholder: '留空则自动探测 tools/scraper/scraper.exe' });
    const concurrency = h('input', { class: 'field', type: 'number', min: '1', max: '8', value: String(cfg.concurrency || 2) });
    const timeout = h('input', { class: 'field', type: 'number', min: '5', max: '180', value: String(cfg.scrapeTimeoutMin || 30) });

    const healthBox = h('div', { class: 'panel', style: 'margin-top:14px' });

    async function checkHealth() {
      clear(healthBox);
      healthBox.append(h('div', { class: 'panel-title', text: '环境自检' }));
      setBusy(true);
      try {
        const health = await call('ScraperHealth');
        setScraperStatus(
          health.chrome?.found === false ? 'error' : 'ready',
          health.chrome?.found === false ? '缺少 Chrome' : `刮削器 ${health.scraper || '就绪'}`,
        );
        healthBox.append(h('div', { class: 'detail-rows' }, [
          h('div', { class: 'detail-row' }, ['刮削器：', h('b', { text: health.scraper || '—' })]),
          h('div', { class: 'detail-row' }, ['Python：', h('b', { text: health.python || '—' })]),
          h('div', { class: 'detail-row' }, ['协议：', h('b', { text: `v${health.v}` })]),
          h('div', { class: 'detail-row' }, ['Chrome：', h('b', { text: health.chrome?.found ? (health.chrome.path || '已安装') : '未找到' })]),
          h('div', { class: 'detail-row' }, ['驱动：', h('b', { text: health.driver?.ready ? '就绪' : (health.driver?.writable ? '首次刮削时下载' : '目录不可写') })]),
        ]));
      } catch (error) {
        setScraperStatus('unknown', '刮削器未配置');
        healthBox.append(h('div', { class: 'badge broken', text: '自检失败' }));
        healthBox.append(h('div', { class: 'panel-desc', text: error?.message || String(error) }));
        healthBox.append(h('div', { class: 'panel-desc', style: 'margin-top:6px', text: '请确认已打包 scraper.exe，或在上方手动指定路径。' }));
      } finally {
        setBusy(false);
      }
    }

    const panel = h('div', { class: 'panel' }, [
      h('div', { class: 'panel-title', text: 'scraper.exe' }),
      fieldRow('路径', h('div', { style: 'display:flex;gap:8px;width:100%' }, [
        scraperPath,
        h('button', {
          type: 'button',
          class: 'btn secondary small',
          onclick: async () => {
            const path = await call('PickExecutable', '选择 scraper.exe');
            if (path) scraperPath.value = path;
          },
        }, [h('span', { class: 'ms', text: 'folder_open' }), h('span', { text: '浏览…' })]),
      ]), '独立子进程，stdin/stdout NDJSON 通信'),
      fieldRow('并发数（1–8）', concurrency, '每个任务约一个 Chrome 实例，过高会提高验证码失败率'),
      fieldRow('单任务超时（分钟）', timeout, '无事件超过该时间视为挂起并终止'),
      h('div', { style: 'display:flex;gap:8px;margin-top:12px;flex-wrap:wrap' }, [
        h('button', {
          type: 'button',
          class: 'btn primary',
          onclick: async () => {
            setBusy(true);
            try {
              await call('SaveConfig', {
                concurrency: Number(concurrency.value) || 2,
                scrapeTimeoutMin: Number(timeout.value) || 30,
                scraperPath: scraperPath.value.trim(),
                ffprobePath: cfg.ffprobePath || '',
                playerPath: cfg.playerPath || '',
                theme: cfg.theme || 'dark',
              });
              toast('已保存刮削器设置', 'ok');
              await checkHealth();
            } catch (error) {
              reportError('保存失败', error);
            } finally {
              setBusy(false);
            }
          },
        }, [h('span', { class: 'ms', text: 'save' }), h('span', { text: '保存' })]),
        h('button', {
          type: 'button',
          class: 'btn secondary',
          onclick: checkHealth,
        }, [h('span', { class: 'ms', text: 'health_and_safety' }), h('span', { text: '重新自检' })]),
      ]),
    ]);

    body.append(panel, healthBox);
    await checkHealth();
  }

  // ── 播放器设置 ─────────────────────────────────────────────────────────
  async function renderPlayer() {
    updateHeader();
    clear(body);
    const cfg = await call('GetConfig');
    const playerPath = h('input', { class: 'field', value: cfg.playerPath || '', placeholder: '留空使用系统默认关联' });

    body.append(h('div', { class: 'panel' }, [
      h('div', { class: 'panel-title', text: '外部播放器' }),
      h('div', { class: 'panel-desc', style: 'margin-bottom:12px', text: 'WebView2 内嵌播放对 HEVC / 10bit 不可靠。外部播放器是一等公民路径：PotPlayer / MPC-HC / VLC / 系统默认。' }),
      fieldRow('可执行文件', h('div', { style: 'display:flex;gap:8px;width:100%' }, [
        playerPath,
        h('button', {
          type: 'button',
          class: 'btn secondary small',
          onclick: async () => {
            const path = await call('PickExecutable', '选择播放器可执行文件');
            if (path) playerPath.value = path;
          },
        }, [h('span', { class: 'ms', text: 'folder_open' }), h('span', { text: '浏览…' })]),
      ]), '支持续播参数：PotPlayer /seek、MPC-HC/VLC --start'),
      h('div', { style: 'display:flex;gap:8px;margin-top:12px' }, [
        h('button', {
          type: 'button',
          class: 'btn primary',
          onclick: async () => {
            try {
              await call('SaveConfig', {
                concurrency: cfg.concurrency,
                scrapeTimeoutMin: cfg.scrapeTimeoutMin,
                scraperPath: cfg.scraperPath || '',
                ffprobePath: cfg.ffprobePath || '',
                playerPath: playerPath.value.trim(),
                theme: cfg.theme || 'dark',
              });
              toast('已保存播放器设置', 'ok');
            } catch (error) {
              reportError('保存失败', error);
            }
          },
        }, [h('span', { class: 'ms', text: 'save' }), h('span', { text: '保存' })]),
        h('button', {
          type: 'button',
          class: 'btn secondary',
          onclick: () => {
            playerPath.value = '';
            toast('已清空，保存后将使用系统默认', 'info');
          },
        }, [h('span', { class: 'ms', text: 'link_off' }), h('span', { text: '使用系统默认' })]),
      ]),
    ]));
  }

  // ── 环境诊断 ───────────────────────────────────────────────────────────
  async function renderEnv() {
    updateHeader();
    clear(body);
    const host = h('div', {});
    body.append(host);

    async function run() {
      clear(host);
      setBusy(true);
      host.append(h('div', { class: 'panel-desc', text: '检查中…' }));
      try {
        const report = await call('DiagnoseEnv');
        const paths = await call('Paths');
        clear(host);

        const rows = [
          ['平台', report.platform],
          ['Go', report.goVersion],
          ['配置目录', paths.configDir],
          ['配置文件', paths.configFile],
          ['ffprobe', report.ffprobeOk ? report.ffprobePath : '未找到（可选）'],
          ['scraper.exe', report.scraperExeOK ? report.scraperExePath : '未找到'],
          ['播放器', report.playerPath || '系统默认'],
          ['Chrome', report.scraperHealth?.chrome?.found ? (report.scraperHealth.chrome.path || '已安装') : '—'],
          ['刮削器版本', report.scraperHealth?.scraper || '—'],
          ['协议', report.scraperHealth ? `v${report.scraperHealth.v}` : '—'],
          ['临时目录可写', report.tempWritable ? '是' : '否'],
        ];

        host.append(h('div', { class: 'panel' }, [
          h('div', { style: 'display:flex;gap:8px;margin-bottom:12px' }, [
            h('button', {
              type: 'button', class: 'btn primary small', onclick: run,
            }, [h('span', { class: 'ms', text: 'refresh' }), h('span', { text: '重新检查' })]),
            h('button', {
              type: 'button',
              class: 'btn secondary small',
              onclick: async () => {
                const text = rows.map(([k, v]) => `${k}: ${v}`).join('\n')
                  + (report.scraperError ? `\nscraperError: ${report.scraperError}` : '');
                try {
                  await navigator.clipboard.writeText(text);
                  toast('诊断信息已复制', 'ok');
                } catch {
                  toast(text.slice(0, 200), 'info');
                }
              },
            }, [h('span', { class: 'ms', text: 'content_copy' }), h('span', { text: '复制诊断' })]),
          ]),
          h('div', { class: 'detail-rows' }, rows.map(([k, v]) => h('div', { class: 'detail-row' }, [
            `${k}：`,
            h('b', { class: String(v).startsWith('/') || String(v).includes('\\') ? 'mono' : '', text: String(v) }),
          ]))),
          report.scraperError
            ? h('div', {
              style: 'margin-top:10px;padding:8px;border-radius:8px;background:var(--warn-bg);color:var(--warn);font-size:12px',
              text: report.scraperError,
            })
            : null,
        ]));
      } catch (error) {
        clear(host);
        host.append(emptyState({
          icon: 'error',
          title: '诊断失败',
          desc: error?.message || String(error),
        }));
      } finally {
        setBusy(false);
      }
    }

    await run();
  }

  // ── 外观 ───────────────────────────────────────────────────────────────
  async function renderAppearance() {
    updateHeader();
    clear(body);
    const paths = await call('Paths');
    const cfg = await call('GetConfig');

    const makeThemeBtn = (value, label) => h('button', {
      type: 'button',
      class: `btn ${document.documentElement.dataset.theme === value ? 'primary' : 'secondary'}`,
      onclick: async () => {
        applyTheme(value);
        try {
          await call('SaveConfig', {
            concurrency: cfg.concurrency,
            scrapeTimeoutMin: cfg.scrapeTimeoutMin,
            scraperPath: cfg.scraperPath || '',
            ffprobePath: cfg.ffprobePath || '',
            playerPath: cfg.playerPath || '',
            theme: value,
          });
        } catch {
          /* theme still applies locally */
        }
        await renderAppearance();
        window.dispatchEvent(new CustomEvent('cinevault:theme-changed', { detail: value }));
      },
      text: label,
    });

    body.append(h('div', { class: 'panel' }, [
      h('div', { class: 'panel-title', text: '主题' }),
      h('div', { class: 'panel-desc', style: 'margin-bottom:12px', text: '两套主题仅切换色彩变量；字体统一为 Space Grotesk + Inter + JetBrains Mono。' }),
      h('div', { style: 'display:flex;gap:8px;flex-wrap:wrap' }, [
        makeThemeBtn('dark', '深色 Obsidian'),
        makeThemeBtn('light', '浅色 Precision'),
      ]),
    ]));

    body.append(h('div', { class: 'panel', style: 'margin-top:14px' }, [
      h('div', { class: 'panel-title', text: '配置位置' }),
      h('div', { class: 'detail-rows' }, [
        h('div', { class: 'detail-row' }, ['目录：', h('b', { class: 'mono', text: paths.configDir })]),
        h('div', { class: 'detail-row' }, ['文件：', h('b', { class: 'mono', text: paths.configFile })]),
      ]),
    ]));

    body.append(h('div', { class: 'panel', style: 'margin-top:14px' }, [
      h('div', { class: 'panel-title', text: '首次引导' }),
      h('div', { class: 'panel-desc', style: 'margin-bottom:10px', text: '重置后下次启动（无视频库时）会再次显示 3 屏引导。' }),
      h('button', {
        type: 'button',
        class: 'btn secondary small',
        onclick: () => {
          resetOnboarding();
          toast('已重置引导标记', 'ok');
        },
      }, [h('span', { class: 'ms', text: 'replay' }), h('span', { text: '重置引导' })]),
    ]));
  }

  // ── 关于（占位） ───────────────────────────────────────────────────────
  async function renderAbout() {
    updateHeader();
    clear(body);
    body.append(h('div', { class: 'panel' }, [
      h('div', { class: 'panel-title', text: 'CineVault Workstation' }),
      h('div', { class: 'panel-desc', text: '本地视频库桌面端：刮削元数据 + 浏览观看。Windows x64 · Go + Wails v2 + WebView2。' }),
      h('div', { class: 'detail-rows', style: 'margin-top:12px' }, [
        h('div', { class: 'detail-row' }, ['App 版本：', h('b', { text: '0.1.0' })]),
        h('div', { class: 'detail-row' }, ['协议版本：', h('b', { text: '1' })]),
        h('div', { class: 'detail-row' }, ['交付形态：', h('b', { text: 'video-library.exe + scraper.exe' })]),
      ]),
    ]));
    body.append(emptyState({
      icon: 'system_update',
      title: '自动更新第三期提供',
      desc: '当前版本请手动替换同目录下的两个 exe。',
    }));
  }

  const renderers = {
    library: renderLibrary,
    scraper: renderScraper,
    player: renderPlayer,
    env: renderEnv,
    appearance: renderAppearance,
    about: renderAbout,
  };

  return {
    el: root,
    async onActivate() {
      await (renderers[section] || renderLibrary)();
    },
    async onLibraryChange() {
      if (section === 'library') await renderLibrary();
    },
  };
}
