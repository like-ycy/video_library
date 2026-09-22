# Video Library 前端架构迁移说明

## 目标

将当前 Wails 原生 JavaScript 前端迁移为：

- Go
- Wails
- React
- Tailwind CSS
- shadcn/ui
- Radix UI

目标是保留现有 Go 后端能力和业务流程，替换前端渲染层、组件样式和交互实现。

## 现状

当前项目结构：

- `go/`：Wails 应用与 Go 后端
- `go/frontend/`：原生 JavaScript、HTML、CSS 前端
- `go/frontend/src/views/`：业务页面
- `go/frontend/src/components/`：业务组件
- `go/frontend/src/styles/`：全局 token、shell、组件和页面样式
- `design-system/`：独立设计系统展示页

当前前端没有 React、Tailwind、Radix 或 shadcn/ui。

## 目标架构

```text
go/
├── app.go                  # Wails 暴露给前端的方法
├── internal/                # Go 业务逻辑
└── frontend/
    ├── package.json
    ├── vite.config.js
    ├── tailwind.config.js
    ├── postcss.config.js
    ├── index.html
    └── src/
        ├── main.jsx
        ├── App.jsx
        ├── api/
        │   └── wails.js
        ├── components/
        │   ├── ui/          # shadcn/ui 组件
        │   ├── shell/       # 标题栏、侧边栏、页面框架
        │   └── media/       # 视频库业务组件
        ├── pages/
        │   ├── library/
        │   ├── watch/
        │   ├── scrape/
        │   ├── tasks/
        │   ├── issues/
        │   └── settings/
        ├── hooks/
        ├── lib/
        │   ├── utils.js
        │   └── constants.js
        ├── styles/
        │   ├── globals.css
        │   └── tokens.css
        └── routes.jsx
```

## 依赖策略

只加入实际使用的依赖：

```json
{
  "dependencies": {
    "@radix-ui/react-dialog": "latest",
    "@radix-ui/react-dropdown-menu": "latest",
    "@radix-ui/react-select": "latest",
    "@radix-ui/react-tabs": "latest",
    "@radix-ui/react-toast": "latest",
    "class-variance-authority": "latest",
    "clsx": "latest",
    "lucide-react": "latest",
    "react": "latest",
    "react-dom": "latest",
    "tailwind-merge": "latest"
  },
  "devDependencies": {
    "@vitejs/plugin-react": "latest",
    "autoprefixer": "latest",
    "postcss": "latest",
    "tailwindcss": "latest",
    "vite": "latest"
  }
}
```

不要安装完整组件库。shadcn/ui 组件源码放入 `src/components/ui/`，按页面需要添加。

如果某个 Radix primitive 没有实际需求，不要提前安装。

## Tailwind 与 token 规则

`design-system/` 是视觉来源。迁移时把它的 token 映射为 CSS variables，再让 Tailwind 使用语义变量。

核心 token：

```css
:root {
  --background: 247 248 250;
  --foreground: 29 33 41;
  --card: 255 255 255;
  --card-foreground: 29 33 41;
  --popover: 255 255 255;
  --popover-foreground: 29 33 41;
  --primary: 22 93 255;
  --primary-foreground: 255 255 255;
  --secondary: 242 243 245;
  --secondary-foreground: 29 33 41;
  --muted: 242 243 245;
  --muted-foreground: 78 89 105;
  --accent: 232 243 255;
  --accent-foreground: 14 66 210;
  --destructive: 245 63 63;
  --destructive-foreground: 255 255 255;
  --border: 229 230 235;
  --input: 201 205 212;
  --ring: 22 93 255;
  --radius: 0.375rem;
}
```

要求：

- 页面组件不得直接写产品级 hex 颜色
- 所有颜色通过 semantic token 使用
- 深色主题覆盖同一组 token
- 统一 4px 间距基准
- 默认控件高度 32px，小控件 28px，大按钮 40px
- 默认圆角 6px，卡片 8px，面板 10px，弹窗 12px
- 正文最小 14px
- 所有交互控件必须有 `focus-visible`

## shadcn/ui 组件范围

第一阶段只实现这些组件：

- `Button`
- `Input`
- `Label`
- `Select`
- `DropdownMenu`
- `Dialog`
- `Tabs`
- `Badge`
- `Toast`
- `Tooltip`
- `Separator`
- `ScrollArea`

组件要求：

- 保留 shadcn 的可组合 API
- 保留 Radix 的键盘操作、焦点管理和 ARIA 行为
- 视觉 token 必须来自 `design-system/`
- 不复制整套未使用组件
- 图标统一使用 `lucide-react`
- 不使用 emoji 作为结构图标

## Wails API 约束

Go 后端 API 继续作为唯一数据来源。

前端通过一个 API 层调用 Wails 绑定：

```js
// src/api/wails.js
import {
  ListLibraries,
  AddLibrary,
  RemoveLibrary,
  ScanLibrary,
  RebuildIndex,
} from '../../wailsjs/go/main/App';

export const libraryApi = {
  list: ListLibraries,
  add: AddLibrary,
  remove: RemoveLibrary,
  scan: ScanLibrary,
  rebuild: RebuildIndex,
};
```

规则：

- 页面组件不能直接散落调用 `wailsjs/go/...`
- 不新增第二套业务状态源
- 异步操作必须有 loading、success、error 状态
- 破坏性操作使用 `Dialog` 确认
- 事件订阅必须在 `useEffect` 清理
- 保持现有 Go 方法签名，除非迁移确实需要修改

## 路由与页面迁移顺序

不要一次性重写所有页面。

### 阶段 1：基础设施

- React/Vite 入口
- Tailwind 配置
- token 和主题切换
- `cn()` 工具
- shadcn 基础组件
- Wails API 封装
- App shell

验收：应用能启动，明暗主题可切换，标题栏和侧边栏可用。

### 阶段 2：视频库管理页

迁移当前截图对应的页面：

- 页面标题
- 添加视频库按钮
- 已配置视频库卡片
- 更新索引
- 重建索引
- 移除确认弹窗
- 空状态和错误状态

验收：功能与当前页面一致，视觉完全使用新组件。

### 阶段 3：观影页面

- 全部影片
- 演员筛选
- 继续观看
- 最近播放
- 媒体卡片
- 详情弹窗

### 阶段 4：刮削与任务页面

- 扫描与待处理
- 任务监控
- 异常与修复
- 进度、状态标签、批量操作

### 阶段 5：设置页面

- 视频库设置
- 刮削器设置
- 播放器设置
- 外观与配置文件
- 环境诊断
- 关于与更新

旧页面只有在对应新页面验收通过后才删除。

## 视觉验收标准

每个迁移页面必须满足：

- 与 `design-system/index.html` 的 token 和组件状态一致
- 主按钮只有一个视觉主层级
- 侧栏 active 状态清晰但不过度高亮
- hover、pressed、disabled、focus-visible 状态完整
- 所有按钮最小点击区域不低于 32px；关键操作不低于 40px
- 文本对比度达到 WCAG AA
- 颜色不是唯一状态表达方式
- 弹窗支持 Escape 关闭和焦点回收
- 下拉菜单支持键盘导航
- `prefers-reduced-motion` 下不播放非必要动画
- 375px、768px、1024px、1440px 宽度下无水平溢出

## 构建与运行

开发：

```bash
cd go
wails dev
```

前端单独开发：

```bash
cd go/frontend
npm install
npm run dev
```

生产构建：

```bash
cd go
wails build
```

迁移期间每个阶段都必须验证：

```bash
npm run build
wails build
```

## 禁止事项

- 不把 React 页面和旧原生页面混在同一个入口长期维护
- 不复制完整 shadcn/ui 仓库
- 不引入全量 UI 框架替代 shadcn/ui
- 不在业务页面硬编码颜色、阴影和圆角
- 不让页面直接操作 Wails 绑定
- 不为了迁移删除 Go 后端测试或修改业务行为
- 不一次性删除旧前端，必须按页面逐步替换

## 完成定义

迁移完成后：

1. `go/frontend` 使用 React 入口构建。
2. 页面全部使用 Tailwind + shadcn/ui 组件。
3. Wails API 仍由 Go 提供，业务行为保持一致。
4. `design-system/` 是唯一视觉 token 来源。
5. 旧原生 JS 页面、旧组件样式和重复 token 被删除。
6. `npm run build` 和 `wails build` 均通过。
7. 关键页面完成浅色、深色、键盘操作和窗口尺寸验收。
