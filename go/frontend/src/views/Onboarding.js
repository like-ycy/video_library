import { call } from '../api.js';
import { clear, h, reportError, setBusy, toast } from '../ui.js';
import { emptyState, pageHeader } from '../components/shell.js';

const ONBOARD_KEY = 'cinevault.onboarded';

/** 首次使用引导：3 屏 */
export function createOnboarding(state, { onDone }) {
  const root = h('div', { class: 'page onboard' });
  let step = 0;

  const steps = [
    {
      icon: 'video_library',
      title: '添加视频库',
      desc: '选择一个根目录，其下应为「演员名/视频文件.mp4」。路径建议为 Windows 形式，如 D:\\Videos\\Library。',
      primary: '添加第一个视频库',
    },
    {
      icon: 'auto_fix_high',
      title: '配置刮削器',
      desc: '刮削器是独立的 scraper.exe 子进程。可跳过，之后在「刮削器设置」再配。需要本机 Chrome。',
      primary: '选择 scraper.exe',
      secondary: '跳过，稍后配置',
    },
    {
      icon: 'manage_search',
      title: '扫描并开始',
      desc: '扫描视频库建立索引，然后在「扫描与待处理」勾选条目开始刮削。完成后详情页会出现元数据与剧照。',
      primary: '立即扫描入库',
      secondary: '进入演员页',
    },
  ];

  function render() {
    clear(root);
    const stepDef = steps[step];
    const canNext = step === 0 ? state.libraries.length > 0 : true;

    root.append(pageHeader({
      title: '欢迎使用视频库',
      sub: `第 ${step + 1} / ${steps.length} 步`,
    }));

    root.append(h('div', { class: 'page-body' }, [
      emptyState({
        icon: stepDef.icon,
        title: stepDef.title,
        desc: stepDef.desc,
        actions: [
          {
            label: stepDef.primary,
            primary: true,
            icon: step === 2 ? 'sync' : 'arrow_forward',
            onClick: async () => {
              if (step === 0) {
                setBusy(true);
                try {
                  const rootPath = await call('PickLibraryRoot');
                  if (!rootPath) {
                    setBusy(false);
                    return;
                  }
                  await call('AddLibrary', rootPath);
                  toast('视频库已添加', 'ok');
                  window.dispatchEvent(new CustomEvent('cinevault:libraries-changed'));
                } catch (error) {
                  reportError('添加失败', error);
                } finally {
                  setBusy(false);
                }
                render();
                return;
              }
              if (step === 1) {
                setBusy(true);
                try {
                  const path = await call('PickExecutable', '选择 scraper.exe');
                  if (path) {
                    const cfg = await call('GetConfig');
                    await call('SaveConfig', { ...cfg, scraperPath: path });
                    toast('刮削器路径已保存', 'ok');
                  }
                } catch (error) {
                  reportError('配置失败', error);
                } finally {
                  setBusy(false);
                }
                step = 2;
                render();
                return;
              }
              // step 2
              if (state.libraryId) {
                setBusy(true);
                try {
                  await call('ImportLibrary', state.libraryId);
                  toast('索引已更新', 'ok');
                } catch (error) {
                  reportError('扫描失败', error);
                } finally {
                  setBusy(false);
                }
              }
              finish();
            },
          },
          step > 0
            ? {
              label: '上一步',
              icon: 'arrow_back',
              onClick: () => {
                step -= 1;
                render();
              },
            }
            : null,
          stepDef.secondary
            ? {
              label: stepDef.secondary,
              icon: 'skip_next',
              onClick: () => {
                if (step < steps.length - 1) {
                  step += 1;
                  render();
                } else {
                  finish();
                }
              },
            }
            : null,
          !canNext && step === 0
            ? {
              label: '完成引导',
              icon: 'check',
              onClick: finish,
            }
            : null,
        ].filter(Boolean),
      }),
    ]));
  }

  function finish() {
    localStorage.setItem(ONBOARD_KEY, '1');
    onDone?.();
  }

  return {
    el: root,
    async onActivate() {
      render();
    },
    async onLibraryChange() {
      if (step === 0) render();
    },
  };
}

export function needsOnboarding() {
  return localStorage.getItem(ONBOARD_KEY) !== '1';
}

export function resetOnboarding() {
  localStorage.removeItem(ONBOARD_KEY);
}
