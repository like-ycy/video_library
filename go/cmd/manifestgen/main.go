// Command manifestgen 生成 Release 附带的组件清单 manifest.json。
//
// 用法（release job，资产下载齐之后执行）：
//
//	cd go && go run ./cmd/manifestgen \
//	  -dir ../release-assets \
//	  -base-url "https://github.com/o/r/releases/download/v1.2.3" \
//	  -require "windows/amd64,darwin/arm64,darwin/amd64" \
//	  -out ../release-assets/manifest.json
//
// 为什么是 Go 程序而不是 CI 里的 shell + jq
// ------------------------------------------
// manifest 的字段名必须与 App 侧解析端（internal/component 的 Manifest）逐字一致。
// 用 shell 拼 JSON，字段名就成了第二份事实，改一边忘另一边时的症状只在用户机器上
// 出现（「组件清单版本不支持」「下载地址为空」），而 CI 永远是一片绿。
//
// 这里直接复用同一个 struct 生成，并且生成后立刻用 component.ParseManifest 与
// Spec 走一遍 App 的解析与校验路径：清单 App 读不懂，构建当场失败。
//
// 输入约定
// --------
//
//	<name>_<version>_<os>_<arch>.zip    组件包
//	<name>_meta_<os>_<arch>.json        该平台产物自述的版本与协议
//
// meta 由构建阶段写入：打包完当场跑一次 `<binary> version`，把输出重定向到文件。
// 于是 manifest 里的版本与协议来自**产物本身自述**，而不是 CI 手里的构建变量 ——
// 两者一旦分离，用户看到的会是「装好了却一直提示可更新」，或版本号与实际能力不符。
package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"videolib/internal/component"
)

func main() {
	var (
		dir     = flag.String("dir", ".", "资产所在目录")
		baseURL = flag.String("base-url", "", "资产下载地址前缀（必填）")
		name    = flag.String("name", "scraper", "组件名")
		binary  = flag.String("binary", "scraper", "包内可执行文件名（不含 .exe）")
		require = flag.String("require", "", "必须齐全的平台，逗号分隔的 goos/goarch")
		only    = flag.String("only", "", "只输出这些平台，逗号分隔的 goos/goarch（用于生成 App 内置的单平台清单）")
		out     = flag.String("out", "", "输出路径，默认 <dir>/manifest.json")
	)
	flag.Parse()

	opts := options{
		dir:      *dir,
		baseURL:  *baseURL,
		name:     *name,
		binary:   *binary,
		required: splitList(*require),
		only:     splitList(*only),
	}
	if *out == "" {
		opts.out = filepath.Join(opts.dir, "manifest.json")
	} else {
		opts.out = *out
	}

	if err := run(opts); err != nil {
		fmt.Fprintf(os.Stderr, "生成组件清单失败：%v\n", err)
		os.Exit(1)
	}
}

func run(opts options) error {
	data, err := build(opts)
	if err != nil {
		return err
	}
	if err := os.WriteFile(opts.out, data, 0o644); err != nil {
		return fmt.Errorf("写入 %s: %w", opts.out, err)
	}
	fmt.Printf("已生成 %s（%d 字节）\n", opts.out, len(data))
	fmt.Println(string(data))
	return nil
}

type options struct {
	dir      string
	baseURL  string
	name     string
	binary   string
	required []string
	// only 非空时只保留这些平台。
	//
	// 用途：把「本平台的包」烘进 App 作为断网兜底。App 只需要自己平台那一份，
	// 全平台的清单塞进去是给别人用的数据，白占体积也白暴露别的平台校验码。
	only []string
	out  string
}

// slugToGOOS 把发布文件名里的平台词映射到 GOOS。
//
// 归档与 meta 的平台段现在直接用 GOOS（darwin），不再写 macos：更新器按
// runtime.GOOS 拼后缀找资产，两套写法并存时，改名会让老版本的自更新静默失效。
// macos 保留给旧资产（历史 Release 上仍是那个名字），读旧清单不会失败。
var slugToGOOS = map[string]string{
	"darwin":  "darwin",
	"macos":   "darwin",
	"windows": "windows",
	"linux":   "linux",
}

// safeAssetName 限定文件名可用字符。
//
// 文件名会原样拼进下载地址，而地址是 App 唯一能用的定位方式：一个空格或 `#`
// 就能让 URL 指向别处或截断，症状是「下载 404」，排查时很难想到是版本号里的字符。
var safeAssetName = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// build 生成 manifest 的字节内容。
func build(opts options) ([]byte, error) {
	if opts.baseURL == "" {
		return nil, errors.New("缺少 -base-url（资产下载地址前缀）")
	}
	// 严格校验会拼进路径的字段：Name/Binary 来自命令行，却会决定扫描哪些文件。
	if err := safeSegment("组件名", opts.name); err != nil {
		return nil, err
	}
	if err := safeSegment("可执行文件名", opts.binary); err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(opts.dir)
	if err != nil {
		return nil, fmt.Errorf("读取目录 %s: %w", opts.dir, err)
	}

	metaPrefix := opts.name + "_meta_"
	var (
		assets   []resolved
		versions = map[string][]string{} // version → 出现在哪些平台
	)
	for _, entry := range entries {
		fileName := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(fileName, metaPrefix) || !strings.HasSuffix(fileName, ".json") {
			continue
		}
		stem := strings.TrimSuffix(strings.TrimPrefix(fileName, metaPrefix), ".json")
		slug, arch, err := splitSlugArch(stem)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", fileName, err)
		}
		goos, ok := slugToGOOS[slug]
		if !ok {
			return nil, fmt.Errorf("%s: 未知平台 %q（已知：%s）", fileName, slug, knownSlugs())
		}

		version, protocol, err := readMeta(filepath.Join(opts.dir, fileName), opts.name)
		if err != nil {
			return nil, err
		}
		versions[version] = append(versions[version], platformKey(goos, arch))

		// 包名按 meta 里的版本拼，而不是按目录里有哪些 zip 去反推。
		// 这样「文件名写的版本」与「产物自述的版本」不一致时，这里直接找不到文件 ——
		// 若反过来按文件名解析版本，那种不一致会被安静地写进清单。
		zipName := fmt.Sprintf("%s_%s_%s_%s.zip", opts.name, version, slug, arch)
		if !safeAssetName.MatchString(zipName) {
			return nil, fmt.Errorf("%s: 包名含不安全字符", zipName)
		}
		zipPath := filepath.Join(opts.dir, zipName)
		if _, err := os.Stat(zipPath); err != nil {
			// 这里最常见的成因不是「忘了上传」，而是构建侧用 tag 版本命名了包、
			// 而产物自述的是另一个版本。把两者同时摆出来，比一句 no such file
			// 有用得多 —— 后者会让人去查上传步骤，而问题在打包命名。
			return nil, fmt.Errorf("找不到组件包 %s（按产物自述的版本 %s 拼名）；目录里同平台的包：%s",
				zipName, version, samePlatformZips(entries, opts.name, slug, arch))
		}
		if err := inspectArchive(zipPath, opts.binary, goos); err != nil {
			return nil, err
		}
		sum, size, err := fileSHA256(zipPath)
		if err != nil {
			return nil, err
		}

		assets = append(assets, resolved{
			goos:     goos,
			goarch:   arch,
			version:  version,
			protocol: protocol,
			fileName: zipName,
			asset: component.Asset{
				URL:    strings.TrimSuffix(opts.baseURL, "/") + "/" + zipName,
				Size:   size,
				SHA256: sum,
			},
		})
	}

	if len(opts.only) > 0 {
		allowed := make(map[string]bool, len(opts.only))
		for _, key := range opts.only {
			allowed[key] = true
		}
		kept := assets[:0]
		for _, a := range assets {
			if allowed[platformKey(a.goos, a.goarch)] {
				kept = append(kept, a)
			}
		}
		if len(kept) == 0 {
			return nil, fmt.Errorf("目录 %s 里没有这些平台的产物：%s（现有：%s）",
				opts.dir, strings.Join(opts.only, ", "), describeVersions(versions))
		}
		// versions 是按全部平台统计的，过滤后要重算，否则「两个平台版本不同」
		// 会因为一个被过滤掉的平台而误报。
		assets = kept
		versions = map[string][]string{}
		for _, a := range assets {
			versions[a.version] = append(versions[a.version], platformKey(a.goos, a.goarch))
		}
	}

	if len(assets) == 0 {
		return nil, fmt.Errorf("目录 %s 里没有任何 %s*.json，组件包无法生成清单", opts.dir, metaPrefix)
	}
	if len(versions) > 1 {
		// 各平台版本不同，说明它们的产物来自不同提交。ManifestComponent 只有
		// 一个 version 字段，硬塞进去必然有一个平台被标成另一版本 —— 用户那边
		// 表现为「刚下完又提示可更新」，永远修不好。
		return nil, fmt.Errorf("各平台产物版本不一致：%s", describeVersions(versions))
	}
	// 协议也只有一个字段，但它的不一致比版本更隐蔽：清单写 v1、包实际是 v2，
	// 装完之后凭据记的是 v1，兼容性检查一路放行，直到 NDJSON 解析时才炸。
	if err := checkProtocolAgreement(assets); err != nil {
		return nil, err
	}

	sort.Slice(assets, func(i, j int) bool {
		return platformKey(assets[i].goos, assets[i].goarch) < platformKey(assets[j].goos, assets[j].goarch)
	})

	item := component.ManifestComponent{
		Version:  assets[0].version,
		Protocol: assets[0].protocol,
		Binary:   opts.binary,
		Assets:   map[string]component.Asset{},
	}
	for _, a := range assets {
		item.Assets[platformKey(a.goos, a.goarch)] = a.asset
	}

	data, err := json.MarshalIndent(component.Manifest{
		Schema:     1,
		Components: map[string]component.ManifestComponent{opts.name: item},
	}, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("序列化清单: %w", err)
	}
	data = append(data, '\n')

	if err := verify(opts, data); err != nil {
		return nil, err
	}
	return data, nil
}

// verify 用 App 侧的解析路径把刚生成的清单读一遍。
//
// 这一步挡的是「生成器自己觉得没问题」：字段名拼错、schema 写成别的值、URL 协议
// 不是 http(s) —— 这些在生成端看起来都正常，只有 App 解析时才暴露，而那时清单
// 已经挂在 Release 上了。
func verify(opts options, data []byte) error {
	manifest, err := component.ParseManifest(data)
	if err != nil {
		return fmt.Errorf("生成的清单无法被 App 解析: %w", err)
	}

	present := map[string]bool{}
	for key := range manifest.Components[opts.name].Assets {
		present[key] = true
	}

	// required 与 only 同时给时，只校验过滤后仍在清单里的平台：
	// 「烘内置清单」的场景下 required 是全平台的（构建侧写死），
	// 而实际产物只有本平台一份。
	required := opts.required
	if len(opts.only) > 0 {
		required = opts.only
	}
	if len(required) == 0 {
		fmt.Fprintf(os.Stderr, "警告：未指定 -require，未校验平台齐全性（现有：%s）\n", strings.Join(sortedKeys(present), ", "))
	}
	var missing []string
	for _, key := range required {
		if !present[key] {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("清单缺少必需平台的构建：%s（现有：%s）", strings.Join(missing, ", "), strings.Join(sortedKeys(present), ", "))
	}

	for key := range present {
		goos, goarch, ok := strings.Cut(key, "/")
		if !ok {
			return fmt.Errorf("平台键格式错误：%q", key)
		}
		// Spec 内部会调用 Validate：URL 协议、校验码格式、大小非负都在这里过一遍。
		if _, err := manifest.Spec(opts.name, goos, goarch); err != nil {
			return fmt.Errorf("清单条目 %s 无效: %w", key, err)
		}
	}
	return nil
}

type resolved struct {
	goos     string
	goarch   string
	version  string
	protocol int
	fileName string
	asset    component.Asset
}

// readMeta 读取构建阶段落盘的产物自述。
//
// 字段名同时认两种写法：刮削器的 `version` 命令输出的是
// {"v":1,"type":"version","scraper":"0.4.3",…}，而通用组件元数据希望叫
// version/protocol。让生成器认两种，CI 就只是把命令输出重定向到文件，
// 不必在 bash 与 pwsh 里各写一份 JSON 解析 —— 那才是两处迟早对不上的地方。
func readMeta(path, name string) (string, int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", 0, fmt.Errorf("读取 %s: %w", filepath.Base(path), err)
	}
	// PowerShell 的 Set-Content 在 5.1 上会带 BOM，Go 的 json 不接受。
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return "", 0, fmt.Errorf("解析 %s: %w", filepath.Base(path), err)
	}

	version := firstString(raw, "version", name)
	if version == "" {
		return "", 0, fmt.Errorf("%s 里没有版本号（找过 %q 与 %q）", filepath.Base(path), "version", name)
	}
	protocol, ok := firstNumber(raw, "protocol", "v")
	if !ok {
		return "", 0, fmt.Errorf("%s 里没有协议版本（找过 %q 与 %q）", filepath.Base(path), "protocol", "v")
	}
	return version, protocol, nil
}

// inspectArchive 确认包里真有可执行文件，且解压后正好落在组件目录顶层。
//
// 用 component.TopLevelDir 而不是自己数层级：解压侧的剥壳规则只有一份，
// 打包侧另写一份的话，两份迟早分叉 —— 分叉的症状是「Release 发布成功、
// 用户装完报缺少可执行文件」，现场完全没有线索指向打包环节。
func inspectArchive(path, binary, goos string) error {
	if !safeAssetName.MatchString(filepath.Base(path)) {
		return fmt.Errorf("%s: 包名含不安全字符", filepath.Base(path))
	}
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("打开组件包 %s: %w", filepath.Base(path), err)
	}
	defer func() { _ = file.Close() }()

	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("读取组件包 %s: %w", filepath.Base(path), err)
	}
	zr, err := zip.NewReader(file, info.Size())
	if err != nil {
		return fmt.Errorf("%s 不是有效的 zip: %w", filepath.Base(path), err)
	}

	want := component.TopLevelDir(zr.File) + binary + exeSuffix(goos)
	names := make([]string, 0, len(zr.File))
	for _, f := range zr.File {
		if f.Name == want && f.Mode().IsRegular() {
			// 可执行位也要核：包内条目是 0644 时解出来就是 0644，装完之后
			// State 报 Corrupt，用户看到的是「装好了却跑不起来」。
			// Windows 的可执行性由扩展名决定，没有权限位这一说。
			if goos != "windows" && f.Mode().Perm()&0o111 == 0 {
				return fmt.Errorf("%s 里的 %s 没有可执行权限（mode %v）；zip 打包时要保留权限位（macOS 用 zip -y）",
					filepath.Base(path), want, f.Mode())
			}
			return nil
		}
		if len(names) < 5 {
			names = append(names, f.Name)
		}
	}
	// 报错要带上实际看到的名字：只说「缺少可执行文件」的话，无从判断是没打进去、
	// 名字不对，还是多包了一层目录。
	return fmt.Errorf("%s 里没有 %s（解压后应落在顶层）；包内前几条：%s",
		filepath.Base(path), want, strings.Join(names, ", "))
}

func fileSHA256(path string) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, fmt.Errorf("打开 %s: %w", filepath.Base(path), err)
	}
	defer func() { _ = file.Close() }()

	hasher := sha256.New()
	size, err := io.Copy(hasher, file)
	if err != nil {
		return "", 0, fmt.Errorf("读取 %s: %w", filepath.Base(path), err)
	}
	return hex.EncodeToString(hasher.Sum(nil)), size, nil
}

// splitSlugArch 从 "darwin_arm64" 这种后缀里切出平台与架构。
func splitSlugArch(stem string) (slug, arch string, err error) {
	idx := strings.LastIndexByte(stem, '_')
	if idx <= 0 || idx == len(stem)-1 {
		return "", "", fmt.Errorf("文件名应形如 <os>_<arch>，实际 %q", stem)
	}
	slug, arch = stem[:idx], stem[idx+1:]
	// slug 本身不含分隔符，所以再切一次必须失败；否则说明格式不是预期的三段式。
	if strings.ContainsRune(slug, '_') {
		return "", "", fmt.Errorf("文件名应形如 <os>_<arch>，实际 %q", stem)
	}
	return slug, arch, nil
}

// checkProtocolAgreement 要求每个平台的产物自述协议一致。
//
// 比版本不一致更隐蔽：清单里写一个协议值，包实际是另一个，装完之后凭据记的
// 也是清单里的值 —— 兼容性检查一路放行，直到 NDJSON 解析时才炸，而那时用户
// 看到的是「刮削器无响应」。
func checkProtocolAgreement(assets []resolved) error {
	first := assets[0]
	var drift []string
	for _, a := range assets[1:] {
		if a.protocol != first.protocol {
			drift = append(drift, fmt.Sprintf("%s=v%d", platformKey(a.goos, a.goarch), a.protocol))
		}
	}
	if len(drift) == 0 {
		return nil
	}
	return fmt.Errorf("各平台产物协议版本不一致：%s=v%d，%s",
		platformKey(first.goos, first.goarch), first.protocol, strings.Join(drift, "，"))
}

// samePlatformZips 列出目录里属于同一平台的包名，用于「找不到期望版本」时的诊断。
func samePlatformZips(entries []os.DirEntry, name, slug, arch string) string {
	suffix := "_" + slug + "_" + arch + ".zip"
	var found []string
	for _, entry := range entries {
		fileName := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(fileName, name+"_") || !strings.HasSuffix(fileName, suffix) {
			continue
		}
		found = append(found, fileName)
	}
	if len(found) == 0 {
		return "无"
	}
	sort.Strings(found)
	return strings.Join(found, ", ")
}

func platformKey(goos, goarch string) string { return goos + "/" + goarch }

func exeSuffix(goos string) string {
	if goos == "windows" {
		return ".exe"
	}
	return ""
}

func safeSegment(what, value string) error {
	switch {
	case value == "":
		return fmt.Errorf("%s为空", what)
	case strings.ContainsAny(value, `/\`), strings.ContainsRune(value, 0), value == ".", value == "..":
		return fmt.Errorf("%s非法：%q", what, value)
	}
	return nil
}

func firstString(raw map[string]any, keys ...string) string {
	for _, key := range keys {
		if s, ok := raw[key].(string); ok && s != "" {
			return s
		}
	}
	return ""
}

func firstNumber(raw map[string]any, keys ...string) (int, bool) {
	for _, key := range keys {
		if f, ok := raw[key].(float64); ok {
			return int(f), true
		}
	}
	return 0, false
}

func splitList(value string) []string {
	var out []string
	for _, part := range strings.Split(value, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func knownSlugs() string {
	slugs := make([]string, 0, len(slugToGOOS))
	for slug := range slugToGOOS {
		slugs = append(slugs, slug)
	}
	sort.Strings(slugs)
	return strings.Join(slugs, ", ")
}

func describeVersions(versions map[string][]string) string {
	parts := make([]string, 0, len(versions))
	for version, platforms := range versions {
		sort.Strings(platforms)
		parts = append(parts, fmt.Sprintf("%s（%s）", version, strings.Join(platforms, ", ")))
	}
	sort.Strings(parts)
	return strings.Join(parts, "；")
}
