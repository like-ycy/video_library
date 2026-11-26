# third_party

随整包分发、但不属于本仓库源码的第三方二进制。布局固定为：

```
third_party/<name>/<os>-<arch>/<可执行文件>
```

`tools/package.sh --full` 会把 `<name>` 整个目录拷进 `dist/<PRODUCT>/tools/<name>/`，
所以目录名就是运行时的组件名（见 docs/packaging-contract.md §4）。

## 现有条目

| 目录 | 内容 | 是否需要入库 |
| --- | --- | --- |
| `ffprobe/<os>-<arch>/ffprobe{,.exe}` | 视频时长与编码探测 | 可选。缺了只影响「时长 / 编码」两个字段，package.sh 会给出警告 |

`ffprobe` 没有随仓库提供：许可证与体积都不适合塞进 git。需要随包分发时，
把对应平台的 ffprobe 按上面的路径放进来（macOS 直接从 `brew --prefix ffmpeg` 里取，
Windows 从 gyan.dev 的静态构建里取），再跑一次 `tools/build.sh --full`。
