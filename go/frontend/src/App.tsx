import { useEffect } from 'react';
import { Button, Input } from 'xwang-ui';

function LegacyApplication() {
  useEffect(() => {
    import('./main.js');
  }, []);

  return null;
}

export default function App() {
  return (
    <div id="app">
      <header className="titlebar" id="titlebar">
        <div className="titlebar-left">
          <div className="brand-block">
            <span className="brand-name">视频库</span>
            <span className="brand-version mono" id="app-version">0.1.0</span>
          </div>
          <span className="titlebar-sep" />
          <div className="library-select-wrap">
            <span className="ms">folder_special</span>
            <select id="library-picker" className="library-select" title="选择视频库">
              <option value="">尚未添加视频库</option>
            </select>
          </div>
          <Button type="button" variant="ghost" size="sm" className="btn ghost small" id="add-library" title="添加视频库">
            <span className="ms">add</span><span>添加库</span>
          </Button>
        </div>

        <div className="titlebar-center">
          <div className="global-search">
            <span className="ms">search</span>
            <Input id="global-search" type="search" placeholder="搜索番号、演员、片名…" autoComplete="off" />
            <kbd>Ctrl+K</kbd>
          </div>
        </div>

        <div className="titlebar-right">
          <Button type="button" variant="default" size="sm" className="btn primary small" id="quick-scan" title="扫描当前视频库">
            <span className="ms">sync</span><span>立即扫描</span>
          </Button>
          <button type="button" className="btn secondary icon" id="theme-toggle" title="切换主题（亮/暗/跟随系统）">
            <span className="ms" id="theme-toggle-icon">brightness_auto</span>
          </button>
          <span className="titlebar-sep" data-win-only="true" />
          <div className="win-ctl-group" id="win-ctl-group">
            <button type="button" className="win-ctl" id="win-min" title="最小化" aria-label="最小化"><span className="ms">remove</span></button>
            <button type="button" className="win-ctl" id="win-max" title="最大化" aria-label="最大化"><span className="ms">crop_square</span></button>
            <button type="button" className="win-ctl close" id="win-close" title="关闭" aria-label="关闭"><span className="ms">close</span></button>
          </div>
        </div>
      </header>

      <div className="shell-body">
        <aside className="sidebar"><nav className="sidebar-nav" id="sidebar-nav" aria-label="主导航" /></aside>
        <main className="content-host" id="content-host" />
      </div>
      <div id="busy-bar" aria-hidden="true" />
      <div id="toast-host" role="status" aria-live="polite" />
      <LegacyApplication />
    </div>
  );
}
