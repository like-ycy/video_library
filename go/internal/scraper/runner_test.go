package scraper

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// 模式通过 Runner.Env 传给子进程，而不是 t.Setenv：后者会把变量留在测试进程
// 自己的环境里，子进程与父进程就分不开了。
const fakeModeEnv = "VIDEOLIB_FAKE_SCRAPER_MODE"
const heartbeatEnv = "VIDEOLIB_FAKE_HEARTBEAT"

const (
	modeOK         = "ok"
	modeDoctor     = "doctor"
	modeBadVersion = "badversion"
	modeNoisy      = "noisy"
	modeStubborn   = "stubborn"
	modeChild      = "child"
	modeCwd        = "cwd"
)

// TestMain 让同一个测试二进制兼作「假刮削器」。
//
// 为什么不用 .sh / .py 脚本当假刮削器：Windows 没有 shebang，shell 脚本那套在
// 那边跑不起来。而 Go 的进程调用路径恰恰最需要在 Windows 上被验证 —— 取消时
// 杀整棵进程树在 Windows 上走的是 Job Object，与 Unix 是完全不同的分支。
//
// 拦在 TestMain 而不是某个 Test* 函数里：这样不依赖测试的执行顺序，也不会
// 因为「先跑到了别的用例」而多跑一堆东西。父进程的测试轮次里 fakeModeEnv 是
// 空的（模式只通过 Runner.Env 传给子进程，从不写进父进程的环境），所以走正常路径。
func TestMain(m *testing.M) {
	if mode := os.Getenv(fakeModeEnv); mode != "" {
		runFakeScraper(mode)
		return // runFakeScraper 必定 os.Exit，不会返回到这里
	}
	os.Exit(m.Run())
}

func runFakeScraper(mode string) {
	switch mode {
	case modeDoctor:
		emitLine(`{"v":1,"type":"doctor","scraper":"fake-0.1","python":"3.14.0",` +
			`"platform":"test","temp_writable":true,` +
			`"chrome":{"found":true,"path":"/chrome"},` +
			`"driver":{"ready":true,"source":"/drv/uc_driver","on_demand":false,` +
			`"dir":"/drv","writable":true}}`)
		os.Exit(0)

	case modeBadVersion:
		emitLine(`{"v":99,"type":"progress","fanha":"x","stage":"search","percent":0.1}`)
		// 故意继续大量输出：管道缓冲区只有 64KB，Go 侧若不排空就会把子进程
		// 堵在写上，而 cmd.Wait() 会永远等下去 —— 正是要复现的那个死锁。
		for i := 0; i < 200000; i++ {
			emitLine(`{"v":99,"type":"progress","fanha":"pad","stage":"search","percent":0.1}`)
		}
		os.Exit(0)

	case modeNoisy:
		fmt.Fprintln(os.Stderr, "seleniumbase 的日志行")
		emitLine(`{"v":1,"type":"done","summary":{"ok":0,"failed":0,"skipped":0}}`)
		os.Exit(0)

	case modeStubborn:
		startStubbornChild()
		emitLine(`{"v":1,"type":"progress","fanha":"x","stage":"search","percent":0.1}`)
		// 永不退出，只能被杀 —— 用来验证取消会杀整棵进程树。
		// 用 Sleep 循环而不是 `select {}`：后者会让 Go 运行时判定所有 goroutine
		// 都在沉睡，直接 panic 报 deadlock，测试就测不到想测的东西了。
		for {
			time.Sleep(time.Hour)
		}

	case modeChild:
		// 心跳：不停写一个文件证明自己活着。被杀之后文件就不再更新。
		path := os.Getenv(heartbeatEnv)
		for i := 0; ; i++ {
			_ = os.WriteFile(path, []byte(strconv.Itoa(i)), 0o644)
			time.Sleep(50 * time.Millisecond)
		}

	case modeCwd:
		// 在子进程当前目录写探针文件，用来证明 Runner.WorkDir 生效。
		_ = os.WriteFile("cwd-probe.txt", []byte("ok"), 0o644)
		emitLine(`{"v":1,"type":"done","summary":{"ok":0,"failed":0,"skipped":0}}`)
		os.Exit(0)

	case modeOK:
		jobs := readStdinJobs()
		if len(jobs) == 0 {
			emitLine(`{"v":1,"type":"done","summary":{"ok":0,"failed":0,"skipped":0}}`)
			os.Exit(0)
		}
		// 与真实刮削器一致：把收到的 job 标识原样回显到每条事件里。
		// 这是事件绑回文件的唯一依据，必须由假刮削器一起验证。
		emitLine(fmt.Sprintf(
			`{"v":1,"type":"progress","fanha":%q,"job":%q,"stage":"search","percent":0.5}`,
			jobs[0].Fanha, jobs[0].ID))
		emitLine(fmt.Sprintf(
			`{"v":1,"type":"item_done","fanha":%q,"job":%q,"data":{"title":"标题一",`+
				`"cover_file":"cover.jpg","shot_files":["images/1.jpg"],"total_shots":2}}`,
			jobs[0].Fanha, jobs[0].ID))

		failed := 0
		if len(jobs) > 1 {
			failed = 1
			emitLine(fmt.Sprintf(
				`{"v":1,"type":"item_failed","fanha":%q,"job":%q,`+
					`"reason":"not_found","detail":"站点未收录"}`,
				jobs[1].Fanha, jobs[1].ID))
		}
		emitLine(fmt.Sprintf(
			`{"v":1,"type":"done","summary":{"ok":1,"failed":%d,"skipped":0}}`, failed))
		os.Exit(0)
	}

	os.Exit(0)
}

func emitLine(line string) {
	fmt.Fprintln(os.Stdout, line)
}

// readStdinJobs 读走 stdin 上的 NDJSON job，顺带验证 Go 侧确实投喂了内容。
func readStdinJobs() []Job {
	var jobs []Job
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var job Job
		if err := json.Unmarshal([]byte(line), &job); err == nil && job.Fanha != "" {
			jobs = append(jobs, job)
		}
	}
	return jobs
}

// startStubbornChild 起一个孙进程并让它一直活着。
//
// 不 Wait：孙进程要活到整棵进程树被杀为止，这正是被测的行为。
func startStubbornChild() {
	// 不带任何参数：子进程靠 TestMain 拦截 fakeModeEnv 直接进入伪装模式，
	// 不会真的去跑测试。
	child := exec.Command(os.Args[0])
	child.Env = append(os.Environ(), fakeModeEnv+"="+modeChild)
	if err := child.Start(); err != nil {
		os.Exit(3)
	}
}

// ── 测试侧 ────────────────────────────────────────────────────────────────

type failedItem struct {
	item   Item
	reason string
	detail string
}

type recorder struct {
	mu       sync.Mutex
	progress []string
	done     []ItemData
	// doneFor / failedFor 记录事件被归属到哪条 job。
	//
	// 光看载荷分不出「事件绑对了文件」还是「绑到了同番号的另一个文件」——
	// 那正是本次要钉住的那个 bug，所以必须连归属一起记下来。
	doneFor   []Item
	failedFor []Item
	failed    []failedItem
	logs      []string
}

func (r *recorder) callbacks() Callbacks {
	return Callbacks{
		Progress: func(item Item, stage string, _ float64) {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.progress = append(r.progress, item.String()+"/"+stage)
		},
		ItemDone: func(item Item, data ItemData) {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.done = append(r.done, data)
			r.doneFor = append(r.doneFor, item)
		},
		ItemFailed: func(item Item, reason, detail string) {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.failed = append(r.failed, failedItem{item, reason, detail})
			r.failedFor = append(r.failedFor, item)
		},
		Log: func(line string) {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.logs = append(r.logs, line)
		},
	}
}

func (r *recorder) logsContain(needle string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, line := range r.logs {
		if strings.Contains(line, needle) {
			return true
		}
	}
	return false
}

// testDir 在模块根下建临时目录，返回绝对路径。
//
// 用绝对路径而不是 t.TempDir()：后者建在系统临时目录下，受限环境里可能被拒绝
// 写入。而必须绝对是因为子进程会拿它当 HOME / 数据目录 —— 相对路径会让子进程
// 按自己的 cwd 解析，写到哪里就说不准了。
func testDir(t *testing.T) string {
	t.Helper()

	base := filepath.Join("..", "..", ".gocache-test")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatalf("创建测试目录: %v", err)
	}
	dir, err := os.MkdirTemp(base, "scraper-")
	if err != nil {
		t.Fatalf("创建临时目录: %v", err)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatalf("解析绝对路径: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(abs) })
	return abs
}

// fakeRunner 返回一个把自己当假刮削器拉起的 Runner。
func fakeRunner(mode string) *Runner {
	return &Runner{
		ExePath: os.Args[0],
		// 参数内容无关紧要 —— TestMain 会拦下来。给个像样的值是为了让日志可读。
		Args:        []string{"scrape"},
		Env:         []string{fakeModeEnv + "=" + mode},
		IdleTimeout: 30 * time.Second,
	}
}

func TestInspectParsesDoctorPayload(t *testing.T) {
	var health Health
	if err := fakeRunner(modeDoctor).Inspect(context.Background(), "doctor", &health); err != nil {
		t.Fatalf("Inspect 失败: %v", err)
	}

	if health.V != ProtocolVersion {
		t.Errorf("协议版本 = %d，期望 %d", health.V, ProtocolVersion)
	}
	if health.Scraper != "fake-0.1" {
		t.Errorf("scraper 版本 = %q", health.Scraper)
	}
	if !health.Chrome.Found {
		t.Error("Chrome 未被识别")
	}
	if !health.Driver.Ready || health.Driver.Dir != "/drv" {
		t.Errorf("驱动字段解析有误：%+v", health.Driver)
	}
	if !health.Healthy() {
		t.Error("这个 payload 应当被判为健康")
	}
}

// TestRunFeedsJobsAndParsesEvents 验证整条 IPC：stdin 投喂、stdout 事件解析、
// 回调分发、汇总计数、退出码。
func TestRunFeedsJobsAndParsesEvents(t *testing.T) {
	rec := &recorder{}
	result, err := fakeRunner(modeOK).Run(context.Background(),
		[]Job{
			{ID: "演员A/IPZZ-001", Fanha: "ipzz-001"},
			{ID: "演员A/IPZZ-002", Fanha: "ipzz-002"},
		}, rec.callbacks())
	if err != nil {
		t.Fatalf("Run 失败: %v", err)
	}

	if result.Canceled {
		t.Error("正常结束不应标记为已取消")
	}
	if result.ExitCode != 0 {
		t.Errorf("退出码 = %d，期望 0", result.ExitCode)
	}
	if result.Summary.OK != 1 || result.Summary.Failed != 1 {
		t.Errorf("汇总 = %+v，期望 ok=1 failed=1", result.Summary)
	}

	// 假刮削器是按收到的 job 数决定输出的，所以这两条同时证明 stdin 投喂成功。
	if len(rec.progress) != 1 || rec.progress[0] != "演员A/IPZZ-001/search" {
		t.Errorf("progress 回调 = %v", rec.progress)
	}
	if len(rec.done) != 1 {
		t.Fatalf("item_done 回调 = %d 次，期望 1 次", len(rec.done))
	}
	if rec.done[0].Title != "标题一" || rec.done[0].TotalShots != 2 {
		t.Errorf("item_done 载荷有误：%+v", rec.done[0])
	}
	// 事件必须绑回「它自己那条 job」，而不是随便挑一条同番号的。
	if rec.doneFor[0].ID != "演员A/IPZZ-001" || rec.doneFor[0].Fanha != "ipzz-001" {
		t.Errorf("item_done 归属有误：%+v", rec.doneFor[0])
	}
	if len(rec.failed) != 1 || rec.failed[0].reason != ReasonNotFound {
		t.Fatalf("item_failed 回调 = %+v", rec.failed)
	}
	if rec.failedFor[0].ID != "演员A/IPZZ-002" {
		t.Errorf("item_failed 归属有误：%+v", rec.failedFor[0])
	}
}

// TestItemStringFallsBackToFanha 钉住旧刮削器的回退显示。
//
// 旧组件不回显 job 标识，此时日志与归属展示只能退回番号 —— 若直接显示空串，
// 日志里就会出现「完成 ：封面=...」这种没法看也没法查的行。
func TestItemStringFallsBackToFanha(t *testing.T) {
	if got := (Item{Fanha: "ipzz-001"}).String(); got != "ipzz-001" {
		t.Errorf("无 ID 时应退回番号，实际 %q", got)
	}
	if got := (Item{ID: "演员A/IPZZ-001", Fanha: "ipzz-001"}).String(); got != "演员A/IPZZ-001" {
		t.Errorf("有 ID 时应优先用 ID，实际 %q", got)
	}
}

func TestRunAppliesWorkDir(t *testing.T) {
	// .app 启动时父进程 CWD 可能是只读的 /；子进程必须落到 WorkDir，
	// 否则 seleniumbase 建 downloaded_files 会 Errno 30。
	dir := testDir(t)
	runner := fakeRunner(modeCwd)
	runner.WorkDir = dir
	rec := &recorder{}

	if _, err := runner.Run(context.Background(), []Job{{Fanha: "x"}}, rec.callbacks()); err != nil {
		t.Fatalf("Run 失败: %v", err)
	}

	probe := filepath.Join(dir, "cwd-probe.txt")
	if _, err := os.Stat(probe); err != nil {
		t.Fatalf("子进程未在 WorkDir %s 写探针文件: %v", dir, err)
	}
}

func TestRunWithNoJobsDoesNotSpawn(t *testing.T) {
	// ExePath 指向不存在的文件：如果它真去启动了，这里就会报错。
	runner := &Runner{ExePath: filepath.Join(testDir(t), "does-not-exist")}

	result, err := runner.Run(context.Background(), nil, Callbacks{})
	if err != nil {
		t.Fatalf("空 job 列表不应启动进程，更不该报错: %v", err)
	}
	if result.Summary.OK != 0 {
		t.Errorf("汇总应为零值，实际 %+v", result.Summary)
	}
}

func TestRunForwardsStderrToLog(t *testing.T) {
	rec := &recorder{}
	if _, err := fakeRunner(modeNoisy).Run(context.Background(),
		[]Job{{Fanha: "x"}}, rec.callbacks()); err != nil {
		t.Fatalf("Run 失败: %v", err)
	}

	// stderr 是 seleniumbase 日志的出口，必须原样转给调用方，
	// 否则出问题时用户看不到任何线索。
	if !rec.logsContain("seleniumbase 的日志行") {
		t.Errorf("stderr 未转发到 Log 回调，收到的日志：%v", rec.logs)
	}
}

// TestRunAbortsOnProtocolVersionMismatch 同时守两件事：
//  1. 协议主版本不匹配时必须上报致命错误，而不是硬着头皮解析
//  2. 提前退出前必须排空 stdout —— 否则子进程会阻塞在写满的管道上，
//     cmd.Wait() 永远等下去，表现为应用无提示卡死
func TestRunAbortsOnProtocolVersionMismatch(t *testing.T) {
	done := make(chan struct{})
	var (
		result Result
		err    error
	)
	go func() {
		defer close(done)
		result, err = fakeRunner(modeBadVersion).Run(context.Background(),
			[]Job{{Fanha: "x"}}, Callbacks{})
	}()

	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("协议不匹配时 Run 卡住了 —— 很可能是既没有终止子进程、" +
			"也没有排空 stdout，于是子进程被堵在写管道上，cmd.Wait() 永不返回")
	}

	if err != nil {
		t.Fatalf("协议不匹配应作为结果上报而不是返回错误: %v", err)
	}
	if result.FatalReason == "" {
		t.Error("应设置 FatalReason")
	}
	if !strings.Contains(result.FatalDetail, "协议版本") {
		t.Errorf("FatalDetail 应说明原因，实际 %q", result.FatalDetail)
	}
}

// TestCancelKillsWholeProcessTree 是这组测试里最重要的一条。
//
// exec.CommandContext 的默认取消只杀直接子进程，Python 拉起的 Chrome 会全部
// 残留 —— 用户点几次取消就能把内存耗光，且没有任何提示。这里用「孙进程心跳
// 文件停止更新」来证明整棵树确实被杀了。
func TestCancelKillsWholeProcessTree(t *testing.T) {
	dir := testDir(t)
	heartbeat := filepath.Join(dir, "heartbeat")

	runner := fakeRunner(modeStubborn)
	runner.Env = append(runner.Env, heartbeatEnv+"="+heartbeat)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	var (
		result Result
		err    error
	)
	go func() {
		defer close(done)
		result, err = runner.Run(ctx, []Job{{Fanha: "x"}}, Callbacks{})
	}()

	// 等孙进程真正开始心跳，否则可能在它启动之前就取消，测不到东西。
	if !waitForFile(heartbeat, 10*time.Second) {
		t.Fatal("孙进程没有开始心跳，测试前提不成立")
	}

	cancel()

	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("取消后 Run 未返回 —— 进程树可能没被杀掉")
	}
	if err != nil {
		t.Fatalf("取消不应作为错误上报: %v", err)
	}
	if !result.Canceled {
		t.Error("Canceled 应为 true")
	}

	// 孙进程若还活着，心跳会在 600ms 内继续变化。
	before := readFileOrEmpty(heartbeat)
	time.Sleep(600 * time.Millisecond)
	after := readFileOrEmpty(heartbeat)
	if before != after {
		t.Fatalf("孙进程仍在运行（心跳 %q → %q）—— 进程树没有被杀干净", before, after)
	}
}

func waitForFile(path string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		first := readFileOrEmpty(path)
		// 要求至少写过两次：只看到一次就判定"已启动"，可能在它真正跑起来之前
		// 就取消，那样测不到进程树。
		if first != "" {
			time.Sleep(150 * time.Millisecond)
			if readFileOrEmpty(path) != first {
				return true
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

func readFileOrEmpty(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

// TestInspectAgainstRealScraper 用真正的刮削器验证一次调用路径。
//
// 找不到冻结产物就跳过 —— 它由 tools/build-python 生成，不是必须存在的。
//
// 沙箱化说明：只设 VIDEOLIB_DATA_DIR 是不够的。如果 dist/ 里放着**旧版产物**
// （不认识那个变量），它会退回按 HOME / LOCALAPPDATA 解析出的默认位置，
// 于是测试会往用户的配置目录里写东西 —— 测试绝不能有这种副作用。
// 所以这几个变量一并不掉。
func TestInspectAgainstRealScraper(t *testing.T) {
	exe := locateFrozenScraper()
	if exe == "" {
		t.Skip("未找到冻结产物，先运行 tools/build-python 再跑本用例")
	}

	sandbox := testDir(t)
	runner := &Runner{
		ExePath: exe,
		Env: []string{
			"VIDEOLIB_DATA_DIR=" + sandbox,
			"HOME=" + sandbox,
			"LOCALAPPDATA=" + sandbox,
			"APPDATA=" + sandbox,
		},
	}

	var health Health
	if err := runner.Inspect(context.Background(), "doctor", &health); err != nil {
		t.Fatalf("对真实刮削器执行 doctor 失败: %v", err)
	}

	if health.V != ProtocolVersion {
		t.Errorf("协议版本不匹配：需要 v%d，实际 v%d", ProtocolVersion, health.V)
	}
	if health.Scraper == "" || health.Python == "" {
		t.Errorf("doctor 未返回版本信息：%+v", health)
	}
	// 核心断言：驱动目录必须正好落在沙箱的 drivers 下。
	//
	// 断言「正好」而不是「包含沙箱」是有意的：旧版产物不认识 VIDEOLIB_DATA_DIR，
	// 会退回按 HOME 解析的 `<沙箱>/Library/Application Support/videolib/drivers`。
	// 那样虽然也没写到用户目录，但说明 dist/ 里是过期的产物 —— 应该重建，
	// 而不是让这条测试含糊地通过。
	want := filepath.Join(sandbox, "drivers")
	if health.Driver.Dir != want {
		t.Errorf("驱动目录 = %q，期望 %q。\n"+
			"若实际值是沙箱下的另一个位置，说明 dist/ 里是过期的冻结产物，"+
			"重新运行 tools/build-python 后再试", health.Driver.Dir, want)
	}
}

func locateFrozenScraper() string {
	base := filepath.Join("..", "..", "..", "python", "dist", "scraper")
	for _, name := range []string{"scraper", "scraper.exe"} {
		candidate := filepath.Join(base, name)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return ""
}
