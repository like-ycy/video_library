/** 侧栏导航定义：固定 13 项、三组。顺序与文案不可随意改。 */

export const NAV_GROUPS = [
  {
    id: 'media',
    label: '观影 · MEDIA',
    items: [
      { id: 'actresses', label: '演员', icon: 'person' },
      { id: 'movies', label: '全部影片', icon: 'movie' },
      { id: 'continue', label: '继续观看', icon: 'play_circle' },
      { id: 'recent', label: '最近播放', icon: 'history' },
    ],
  },
  {
    id: 'scraper',
    label: '刮削 · SCRAPER',
    items: [
      { id: 'scan', label: '扫描与待处理', icon: 'manage_search' },
      { id: 'tasks', label: '任务监控', icon: 'dns' },
      { id: 'issues', label: '异常与修复', icon: 'build_circle' },
    ],
  },
  {
    id: 'settings',
    label: '设置 · SETTINGS',
    items: [
      { id: 'settings-library', label: '视频库管理', icon: 'video_library' },
      { id: 'settings-scraper', label: '刮削器设置', icon: 'auto_fix_high' },
      { id: 'settings-player', label: '播放器设置', icon: 'play_lesson' },
      { id: 'settings-env', label: '环境诊断', icon: 'terminal' },
      { id: 'settings-appearance', label: '外观与配置文件', icon: 'palette' },
      { id: 'settings-about', label: '关于与更新', icon: 'info' },
    ],
  },
];

export const DEFAULT_ROUTE = 'actresses';

export function findNavItem(id) {
  for (const group of NAV_GROUPS) {
    const item = group.items.find((it) => it.id === id);
    if (item) return { group, item };
  }
  return null;
}
