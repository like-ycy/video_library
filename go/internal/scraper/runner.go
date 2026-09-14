package scraper

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Job 是一个待刮削目标，以 NDJSON 写到 Python 的 stdin。
//
// 走 stdin 而不是命令行参数：Windows 的命令行引号转义规则复杂，中文、空格、
// 括号路径都容易出错，而 NDJSON 完全没有这个问题。
type Job struct {
	Fanha string `json:"fanha"`
	Out   string `json:"out"`
	Force bool   `json:"force,omitempty"`
}

// Callbacks 是运行期回调。
//
// 回调在读取 goroutine 上同步调用，实现里不要做阻塞操作，
// 否则会把事件流堵住、进而让看门狗误判为挂起。
type Callbacks struct {
	Progress   func(fanha, stage string, percent float64)
	ItemDone   func(fanha string, data ItemData)
	ItemFailed func(fanha, reason, detail string)
	Log        func(line string)
}

func (c Callbacks) logf(format string, args ...any) {
	if c.Log != nil {
		c.Log(fmt.Sprintf(format, args...))
	}
}

// Result 是一次刮削的汇总。
type Result struct {
	Summary  Summary
	Canceled bool
	ExitCode int
	// FatalReason 非空表示刮削器在开始前就判定无法继续（如环境缺 Chrome），
	// 此时 Summary 全为 0，UI 应展示引导而不是「0 成功 0 失败」。
	FatalReason string
	FatalDetail string
}

// Runner 负责启动、监控并（在需要时）终止刮削器进程。
type Runner struct {
	ExePath     string
	Site        string
	Concurrency int

	// Args 覆盖默认的命令行参数。为空时按 Site / Concurrency 生成。
	//
	// 测试用它把 ExePath 指向一个假刮削器（同一个测试二进制兼作两种角色），
	// 从而在不访问站点、不依赖 Python 的前提下验证整条 IPC 契约。
	Args []string

	// Env 是追加到子进程环境的额外变量。
	//
	// 由调用方追加而不是替换：子进程必须继承父进程的完整环境，否则
	// Python 侧的 %LOCALAPPDATA% 之类的变量会缺失，驱动目录解析不出来。
	Env []string

	// IdleTimeout 是「多久没有任何输出就认为卡死」。
	//
	// seleniumbase 遇到验证码可能长时间静默。没有看门狗的话 UI 会永远停在
	// 「进行中」，用户只能强杀应用。stdout 与 stderr 上的任何输出都会重置
	// 这个计时器，所以正常但缓慢的任务不会被误杀。
	IdleTimeout time.Duration
}

const (
	defaultIdleTimeout = 180 * time.Second

	// maxProtocolLine 放到 8MB。bufio.Scanner 默认 64KB 上限会被
	// 「一个番号的全部截图文件名」这类单行载荷撑爆，症状是「事件流意外中断」，
	// 而真正的原因（行太长）完全看不出来。
	maxProtocolLine = 8 << 20

	// maxStderrLine 是日志行上限，1MB 足够容纳任何 stack trace。
	maxStderrLine = 1 << 20
)

// Run 执行一次刮削，直到结束或被取消。
//
// 取消语义：ctx 被取消后会终止整棵进程树（含 Chrome），返回的 Result.Canceled
// 为 true 且 ExitCode 归零 —— 用户主动取消不是失败，不该上报成失败。
func (r *Runner) Run(ctx context.Context, jobs []Job, cb Callbacks) (Result, error) {
	if r.ExePath == "" {
		return Result{}, errors.New("未配置刮削器路径")
	}
	if len(jobs) == 0 {
		return Result{Summary: Summary{}}, nil
	}
	if err := ctx.Err(); err != nil {
		return Result{Canceled: true}, nil
	}

	// 用 exec.Command 而不是 exec.CommandContext：后者取消时只 Kill 直接
	// 子进程，Chrome 会全部残留。取消由下面的 goroutine 交给 processTree 处理。
	cmd := exec.Command(r.ExePath, r.args()...)
	// Python 侧的编码与缓冲必须显式指定：
	//   缺 PYTHONUNBUFFERED → 块缓冲，Go 长时间读不到事件（表现为进度卡住）
	//   缺 PYTHONIOENCODING → 中文标题乱码
	cmd.Env = append(os.Environ(), r.Env...)
	cmd.Env = append(cmd.Env,
		"PYTHONIOENCODING=utf-8",
		"PYTHONUNBUFFERED=1",
		"PYTHONUTF8=1",
	)
	setProcAttr(cmd)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return Result{}, fmt.Errorf("创建 stdin 管道: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return Result{}, fmt.Errorf("创建 stdout 管道: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return Result{}, fmt.Errorf("创建 stderr 管道: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return Result{}, fmt.Errorf("启动刮削器 %s: %w", r.ExePath, err)
	}

	tree := newProcessTree()
	if err := tree.attach(cmd); err != nil {
		// 拿不到进程树管辖权时仍然继续：刮削本身能跑，只是取消可能留下孤儿
		// Chrome。这种情况必须让用户看见，不能静默降级。
		cb.logf("警告：无法把刮削器纳入进程树管辖（%v），取消后可能残留 Chrome 进程", err)
	}
	defer tree.release()

	var killOnce sync.Once
	kill := func() { killOnce.Do(func() { tree.kill(cmd) }) }

	stopped := make(chan struct{})
	defer close(stopped)
	go func() {
		select {
		case <-ctx.Done():
			kill()
		case <-stopped:
		}
	}()

	activity := newActivityClock()
	watchdogDone := make(chan struct{})
	defer close(watchdogDone)
	go r.watch(cmd, activity, kill, cb, watchdogDone)

	feedStdin(stdin, jobs)

	var stderrWG sync.WaitGroup
	stderrWG.Add(1)
	go func() {
		defer stderrWG.Done()
		drainStderr(stderr, activity, cb)
	}()

	result := consumeEvents(stdout, activity, cb)

	if result.FatalReason != "" {
		// 协议不匹配或致命错误：没必要等它把剩下的 job 跑完。
		kill()
	}

	// 无论正常结束还是提前返回，都必须把 stdout 读干再 Wait。
	//
	// 消费循环可能因为协议不匹配或读错误提前退出，此时子进程往往还在写事件。
	// 不排空的话它会阻塞在写满的管道上永不退出，而 cmd.Wait() 会一直等下去 ——
	// 表现为应用卡死，且没有任何报错。
	_, _ = io.Copy(io.Discard, stdout)

	stderrWG.Wait()
	waitErr := cmd.Wait()

	if err := ctx.Err(); err != nil {
		result.Canceled = true
		return result, nil
	}

	result.ExitCode = exitCode(waitErr)
	// 退出码 1 是「部分失败」，它伴随正常的 done 事件，不是异常。
	if waitErr != nil && result.ExitCode != 1 && result.FatalReason == "" {
		return result, fmt.Errorf("刮削器异常退出（退出码 %d）：%w", result.ExitCode, waitErr)
	}
	return result, nil
}

// Inspect 执行 doctor / version 这类一次性查询命令并解析其 JSON 输出。
//
// 这两个命令的 stdout 约定为「只有 JSON」，所以直接解析第一行即可，
// 不需要事件循环。只要拿到了合法 JSON 就算成功 —— 内容的判定（例如环境是否
// 可用）由调用方依据 payload 决定，而不是依据退出码。
func (r *Runner) Inspect(ctx context.Context, subcommand string, target any) error {
	if r.ExePath == "" {
		return errors.New("未配置刮削器路径")
	}

	cmd := exec.CommandContext(ctx, r.ExePath, subcommand)
	// 与 Run 保持一致地构造环境：只补必需项，其余继承父进程。
	// 两者不一致会让"能跑 scrape 但跑不了 doctor"这类问题极难定位。
	cmd.Env = append(os.Environ(), r.Env...)
	cmd.Env = append(cmd.Env, "PYTHONIOENCODING=utf-8", "PYTHONUTF8=1")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()

	line := firstJSONLine(stdout.Bytes())
	if line == nil {
		detail := lastLines(stderr.String(), 5)
		if detail == "" {
			detail = "无任何输出"
		}
		return fmt.Errorf("刮削器 %s 未返回可解析的 JSON：%s", subcommand, detail)
	}
	if err := json.Unmarshal(line, target); err != nil {
		return fmt.Errorf("解析刮削器 %s 输出: %w", subcommand, err)
	}
	if runErr != nil && exitCode(runErr) == -1 {
		// 进程根本没跑起来（路径错误、无执行权限），这必须报出来。
		return fmt.Errorf("刮削器 %s 无法执行: %w", subcommand, runErr)
	}
	return nil
}

func (r *Runner) args() []string {
	if len(r.Args) > 0 {
		return r.Args
	}

	args := []string{"scrape"}
	if r.Site != "" {
		args = append(args, "--site", r.Site)
	}
	if r.Concurrency > 0 {
		args = append(args, "--concurrency", strconv.Itoa(r.Concurrency))
	}
	return args
}

func (r *Runner) idleTimeout() time.Duration {
	if r.IdleTimeout > 0 {
		return r.IdleTimeout
	}
	return defaultIdleTimeout
}

// watch 在长时间没有任何输出时终止刮削器。
func (r *Runner) watch(
	cmd *exec.Cmd,
	activity *activityClock,
	kill func(),
	cb Callbacks,
	done <-chan struct{},
) {
	idle := r.idleTimeout()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			if elapsed := activity.elapsed(); elapsed > idle {
				cb.logf("已 %s 没有任何输出，判定为挂起，正在终止刮削器",
					elapsed.Round(time.Second))
				kill()
				return
			}
		}
	}
}

// feedStdin 把 job 以 NDJSON 写入子进程 stdin，写完即关闭 —— 对端以 EOF
// 作为「job 投喂结束」的信号。
func feedStdin(stdin io.WriteCloser, jobs []Job) {
	go func() {
		defer stdin.Close()
		// json.Encoder.Encode 会在每个值后追加换行，正好是 NDJSON。
		encoder := json.NewEncoder(stdin)
		for _, job := range jobs {
			if err := encoder.Encode(job); err != nil {
				// 子进程提前退出时管道会断开。这里不单独报错：退出码与事件流
				// 才是判断结果的依据，重复上报只会产生噪音。
				return
			}
		}
	}()
}

func drainStderr(stderr io.Reader, activity *activityClock, cb Callbacks) {
	scanner := bufio.NewScanner(stderr)
	scanner.Buffer(make([]byte, 0, 32<<10), maxStderrLine)
	for scanner.Scan() {
		// stderr 有输出就说明进程还活着，不应被看门狗误杀 ——
		// 验证码阶段可能长时间没有事件，但日志一直在动。
		activity.touch()
		if line := strings.TrimRight(scanner.Text(), "\r"); line != "" {
			cb.logf("%s", line)
		}
	}
}

// consumeEvents 逐行读取协议流。任何一行解析失败都只记录并跳过：
// 一行坏数据不该让整次刮削作废，而已经成功的事件是有价值的。
func consumeEvents(stdout io.Reader, activity *activityClock, cb Callbacks) Result {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64<<10), maxProtocolLine)

	var result Result
	versionChecked := false

	for scanner.Scan() {
		activity.touch()

		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var event Event
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			cb.logf("跳过无法解析的协议行：%s", truncate(line, 200))
			continue
		}

		if !versionChecked {
			versionChecked = true
			if event.V != ProtocolVersion {
				cb.logf("协议版本不匹配：需要 v%d，实际 v%d", ProtocolVersion, event.V)
				result.FatalReason = ReasonInternalError
				result.FatalDetail = fmt.Sprintf(
					"刮削器协议版本为 v%d，本程序需要 v%d。请重新安装刮削组件。",
					event.V, ProtocolVersion)
				return result
			}
		}

		switch event.Type {
		case EventProgress:
			if cb.Progress != nil {
				cb.Progress(event.Fanha, event.Stage, event.Percent)
			}
		case EventItemDone:
			if event.Data != nil && cb.ItemDone != nil {
				cb.ItemDone(event.Fanha, *event.Data)
			}
		case EventItemFailed:
			if cb.ItemFailed != nil {
				cb.ItemFailed(event.Fanha, event.Reason, event.Detail)
			}
		case EventDone:
			if event.Summary != nil {
				result.Summary = *event.Summary
			}
		case EventFatal:
			result.FatalReason = event.Reason
			result.FatalDetail = event.Detail
		default:
			cb.logf("忽略未知事件类型：%s", event.Type)
		}
	}

	if err := scanner.Err(); err != nil {
		cb.logf("读取事件流中断：%v", err)
	}
	return result
}

// activityClock 记录最后一次活动时间，供看门狗判断是否挂起。
type activityClock struct {
	mu   sync.Mutex
	last time.Time
}

func newActivityClock() *activityClock {
	return &activityClock{last: time.Now()}
}

func (c *activityClock) touch() {
	c.mu.Lock()
	c.last = time.Now()
	c.mu.Unlock()
}

func (c *activityClock) elapsed() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return time.Since(c.last)
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}

func firstJSONLine(data []byte) []byte {
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		if trimmed := bytes.TrimSpace(line); len(trimmed) > 0 {
			return trimmed
		}
	}
	return nil
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, " | ")
}

// truncate 按 rune 截断，避免把一个多字节字符切成两半 ——
// 那样日志里会出现乱码，而且可能连累日志本身的编码。
func truncate(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit]) + "…"
}
