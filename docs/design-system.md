# CineVault 设计 Token 清单

状态：第一版展示稿，供 `video_library` 评审。业务页面暂不接入。

## 设计方向

- 参考 Arco Design 的蓝色体系和层级关系。
- 亮色主题使用白色表面、浅灰背景、深蓝文字和蓝色主操作。
- 暗色主题使用近黑背景、深蓝表面、浅色文字和明亮蓝色主操作。
- 尺寸、间距、圆角和组件结构在两个主题间保持一致。
- 颜色按语义命名，业务页面不直接使用色值。

## 颜色 Token

| 语义 | 亮色 | 暗色 | 用途 |
| --- | --- | --- | --- |
| `color-bg` | `#F7F8FA` | `#0B0D10` | 应用和页面背景 |
| `color-titlebar` | `#FFFFFF` | `#101318` | 窗口标题栏 |
| `color-surface` | `#FFFFFF` | `#151A22` | 卡片、面板、输入框 |
| `color-surface-muted` | `#F2F3F5` | `#1D2633` | 次级表面、悬停背景 |
| `color-text` | `#1D2129` | `#F2F3F5` | 主文字 |
| `color-text-secondary` | `#4E5969` | `#C9CDD4` | 辅助文字 |
| `color-text-tertiary` | `#86909C` | `#86909C` | 弱提示、占位文字 |
| `color-border` | `#E5E6EB` | `#2A3441` | 默认边框 |
| `color-border-strong` | `#C9CDD4` | `#3B4756` | 强调边框 |
| `color-primary` | `#165DFF` | `#4080FF` | 主按钮、激活态、链接 |
| `color-primary-hover` | `#4080FF` | `#6AA1FF` | 主色悬停 |
| `color-primary-active` | `#0E42D2` | `#165DFF` | 主色按下 |
| `color-on-primary` | `#FFFFFF` | `#FFFFFF` | 主色上的文字 |
| `color-success` | `#00B42A` | `#23C343` | 成功状态 |
| `color-warning` | `#FF7D00` | `#FFAA2C` | 警告状态 |
| `color-danger` | `#F53F3F` | `#F76969` | 错误、危险操作 |
| `color-info` | `#0E42D2` | `#4080FF` | 信息、进行中 |

状态背景使用对应颜色的低透明度版本，不新增独立色名：`primary-soft`、`success-soft`、`warning-soft`、`danger-soft`、`info-soft`。

## 尺寸 Token

| Token | 值 | 用途 |
| --- | ---: | --- |
| `size-1` 至 `size-6` | `4/8/12/16/20/24px` | 间距基础刻度 |
| `control-sm` | `24px` | 小按钮、小标签 |
| `control-md` | `32px` | 默认按钮、输入框、Select |
| `control-lg` | `40px` | 主要操作、大型表单 |
| `icon-button` | `32px` | 图标按钮 |
| `sidebar-width` | `240px` | 桌面侧边栏 |
| `titlebar-height` | `40px` | Wails 标题栏 |
| `page-padding` | `20px 24px` | 页面内容边距 |
| `panel-padding` | `16px` | 面板内边距 |

## 圆角、字体和层级

| Token | 值 | 用途 |
| --- | ---: | --- |
| `radius-control` | `6px` | 按钮、输入框、Select、导航项 |
| `radius-card` | `8px` | 影片卡片、结果卡片 |
| `radius-panel` | `10px` | 设置面板、任务面板 |
| `radius-dialog` | `12px` | 弹窗、详情层 |
| `radius-pill` | `999px` | Chip、圆形状态 |
| `font-body` | `Inter, PingFang SC, Microsoft YaHei` | 正文 |
| `font-mono` | `JetBrains Mono, Consolas` | 路径、番号、日志、快捷键 |
| `font-xs` | `12px` | 标签、状态、辅助信息 |
| `font-sm` | `13px` | 默认正文 |
| `font-md` | `14px` | 控件和小标题 |
| `font-lg` | `16px` | 面板标题 |
| `font-xl` | `18px` | 页面标题 |

层级顺序：`bg < surface-muted < surface < elevated < dialog`。阴影只用于浮层和弹窗，普通卡片优先使用边框区分。

## 组件规格

| 组件 | 变体/状态 |
| --- | --- |
| Button | `primary`、`secondary`、`ghost`、`danger`、`icon`、`small`；支持 hover、active、disabled |
| Input / Select | 默认、focus、disabled、error；默认高度 32px |
| Checkbox / Switch | 默认、选中、禁用 |
| Nav Item | 默认、hover、active、badge |
| Badge | neutral、success、warning、danger、info |
| Panel / Card | 默认、hover、选中 |
| Empty State | 图标、标题、描述、操作按钮 |
| Toast | info、success、warning、error |
| Dialog | 标题、正文、次要操作、主要操作 |
| Progress | 进度条、统计数字、日志 |
| Table | 表头、行 hover、选中、空状态 |

## 页面映射

| 页面类型 | 主要组件 |
| --- | --- |
| 影片网格 | Page Header、Search、Chip、Select、Card、Badge、Empty State |
| 影片详情 | Overlay、Player、Detail Row、Button、Rating、Shot Grid |
| 扫描与待处理 | Toolbar、Checkbox、Table、Badge、Progress、Log |
| 任务监控 | Panel、Progress、Status、Result List、Empty State |
| 异常与修复 | Summary Card、Issue Group、Danger Button、Empty State |
| 视频库管理 | Panel、Path Field、Primary/Secondary/Danger Button |
| 刮削器/播放器设置 | Form Row、Input、Number Input、Select、Health Panel |
| 外观与配置 | Theme Choice、Panel、Code/Path Text、Reset Button |
| 关于与更新 | Panel、Version Badge、Update Button |

## 实施边界

1. 先在独立展示页确认颜色、尺寸、间距和状态。
2. 展示页确认后，整理 `tokens.css`，再接入 `components.css`。
3. 先改 `video_library`，业务行为保持不变。
4. 稳定后抽取 React + TypeScript + Tailwind 版本的共享组件和 token。
5. 最后迁移 `media-dedupe` 与 `video_cut`。
