// Package terminal 提供后端 Terminal 域（v6 P6-5）。
//
// 一个 Terminal 是一个 buffer-backed shell session：
//   - 创建时指定初始命令（例：cmd.exe / powershell / bash）；
//   - Run 写入一行命令（隐式追加 \n）→ 等待命令结束 → 输出累积；
//   - Read 取当前 buffer（自上次读取后）；
//   - Kill 强制结束。
//
// 设计取舍（与 ds-java v0.1.7+ 对齐，但简化）：
//   - v6 不引入 PTY / interactive shell；只支持"一次性命令"模式；
//     这意味着不支持 vim 之类需要 TTY 的程序，但对 LLM 场景足够。
//   - 每个 Terminal 用一个 goroutine 串行处理命令（避免输出交错）。
//   - buffer 用固定最大行数（默认 1000）。
package terminal

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"
)

// State 是 Terminal 的状态。
type State int

const (
	StateRunning State = iota
	StateStopped
	StateCrashed
)

func (s State) String() string {
	switch s {
	case StateRunning:
		return "running"
	case StateStopped:
		return "stopped"
	case StateCrashed:
		return "crashed"
	default:
		return "unknown"
	}
}

// Terminal 是单个 shell session。
type Terminal struct {
	ID      string    `json:"id"`
	Shell   string    `json:"shell"`   // 启动时执行的命令（cmd.exe / powershell / bash）
	Args    []string  `json:"args,omitempty"`
	Workdir string    `json:"workdir,omitempty"`
	Owner   string    `json:"owner,omitempty"`

	mu       sync.Mutex
	buf      []string // 行缓冲（FIFO；超过 cap 丢最早）
	cap      int
	running  bool
	exited   bool
	cancel   context.CancelFunc
	state    State
	created  time.Time
	updated  time.Time
	readOff  int // 下次 Read 起始位置
}

// New 构造 Terminal；不在此启动。
func New(id, shell string, args []string, workdir, owner string, bufCap int) *Terminal {
	if bufCap <= 0 {
		bufCap = 1000
	}
	return &Terminal{
		ID:      id,
		Shell:   shell,
		Args:    args,
		Workdir: workdir,
		Owner:   owner,
		cap:     bufCap,
		created: time.Now(),
		updated: time.Now(),
		state:   StateStopped,
	}
}

// appendLine 把一行写入 buf。
func (t *Terminal) appendLine(line string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.buf) >= t.cap {
		drop := t.cap / 4
		if drop < 1 {
			drop = 1
		}
		t.buf = t.buf[drop:]
		// readOff 也要回退对应量
		if t.readOff > drop {
			t.readOff -= drop
		} else {
			t.readOff = 0
		}
	}
	t.buf = append(t.buf, line)
	t.updated = time.Now()
}

// Run 在 Terminal 上执行一次命令；返回值 = exit code。
//
// 行为：
//   - 如果 Terminal 已有命令在跑 → 返回 ErrBusy；
//   - 否则把命令作为参数传给 shell（shell -c "cmd" / powershell -Command "cmd" / cmd /c "cmd"）。
func (t *Terminal) Run(ctx context.Context, command string, timeout time.Duration) (int, error) {
	t.mu.Lock()
	if t.running {
		t.mu.Unlock()
		return 0, ErrBusy
	}
	t.running = true
	t.exited = false
	t.mu.Unlock()

	// 启动子进程
	cmd := t.makeCmd(command)
	runCtx, cancel := context.WithCancel(ctx)
	t.mu.Lock()
	t.cancel = cancel
	t.state = StateRunning
	t.mu.Unlock()

	if timeout > 0 {
		var cancelTimeout context.CancelFunc
		runCtx, cancelTimeout = context.WithTimeout(runCtx, timeout)
		defer cancelTimeout()
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.markCrashed(err)
		return 0, fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.markCrashed(err)
		return 0, fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		t.markCrashed(err)
		return 0, fmt.Errorf("start: %w", err)
	}

	var wg sync.WaitGroup
	scan := func(r io.Reader) {
		defer wg.Done()
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 0, 4096), 1<<20)
		for sc.Scan() {
			t.appendLine(sc.Text())
		}
	}
	wg.Add(2)
	go scan(stdout)
	go scan(stderr)
	wg.Wait()

	waitErr := cmd.Wait()
	exit := 0
	if ee, ok := waitErr.(*exec.ExitError); ok {
		exit = ee.ExitCode()
	} else if waitErr != nil {
		// timeout / canceled / 其他
		t.appendLine("[exit] " + waitErr.Error())
	}

	t.mu.Lock()
	t.running = false
	t.exited = true
	if runCtx.Err() != nil {
		t.state = StateCrashed
	} else {
		t.state = StateStopped
	}
	t.cancel = nil
	t.updated = time.Now()
	t.mu.Unlock()

	return exit, nil
}

func (t *Terminal) markCrashed(err error) {
	t.appendLine("[err] " + err.Error())
	t.mu.Lock()
	t.running = false
	t.exited = true
	t.state = StateCrashed
	t.cancel = nil
	t.mu.Unlock()
}

// makeCmd 根据 t.Shell 类型构造命令。
func (t *Terminal) makeCmd(command string) *exec.Cmd {
	var cmd *exec.Cmd
	switch t.Shell {
	case "powershell", "pwsh":
		// powershell.exe -NoLogo -Command "command"
		full := append([]string{"-NoLogo", "-Command", command}, t.Args...)
		cmd = exec.Command("powershell", full...)
	case "cmd":
		// cmd.exe /c "command"
		full := append([]string{"/c", command}, t.Args...)
		cmd = exec.Command("cmd.exe", full...)
	default:
		// bash / sh：-c "command"
		full := append([]string{"-c", command}, t.Args...)
		cmd = exec.Command(t.Shell, full...)
	}
	if t.Workdir != "" {
		cmd.Dir = t.Workdir
	}
	return cmd
}

// Read 返回自上次 Read 后新追加的行。
//
// since 表示"自此行号起"；since=0 表示从头。
// 调用后内部 readOff 前进。
func (t *Terminal) Read(since int) []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if since < 0 {
		since = 0
	}
	if since > len(t.buf) {
		since = len(t.buf)
	}
	out := make([]string, len(t.buf)-since)
	copy(out, t.buf[since:])
	return out
}

// All 返回所有当前 buffer（拷贝）。
func (t *Terminal) All() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.buf) == 0 {
		return nil
	}
	out := make([]string, len(t.buf))
	copy(out, t.buf)
	return out
}

// Kill 取消正在运行的命令（如有）。
func (t *Terminal) Kill() {
	t.mu.Lock()
	c := t.cancel
	t.mu.Unlock()
	if c != nil {
		c()
	}
}

// Snapshot 返回当前状态（用于 list 序列化）。
type Snapshot struct {
	ID       string    `json:"id"`
	Shell    string    `json:"shell"`
	State    State     `json:"state"`
	Owner    string    `json:"owner,omitempty"`
	Buffer   int       `json:"buffer_lines"`
	Created  time.Time `json:"created_at"`
	Updated  time.Time `json:"updated_at"`
}

func (t *Terminal) Snapshot() Snapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	return Snapshot{
		ID:      t.ID,
		Shell:   t.Shell,
		State:   t.state,
		Owner:   t.Owner,
		Buffer:  len(t.buf),
		Created: t.created,
		Updated: t.updated,
	}
}

// Registry 持有 Terminal 集合。
type Registry struct {
	mu       sync.RWMutex
	terminals map[string]*Terminal
	bufCap   int
}

// NewRegistry 构造。
func NewRegistry(bufCap int) *Registry {
	if bufCap <= 0 {
		bufCap = 1000
	}
	return &Registry{
		terminals: make(map[string]*Terminal),
		bufCap:    bufCap,
	}
}

// Create 创建一个 Terminal；返回 id。
func (r *Registry) Create(shell string, args []string, workdir, owner string) (*Terminal, error) {
	if shell == "" {
		return nil, errors.New("terminal: empty shell")
	}
	id, err := newID()
	if err != nil {
		return nil, err
	}
	t := New(id, shell, args, workdir, owner, r.bufCap)
	r.mu.Lock()
	r.terminals[id] = t
	r.mu.Unlock()
	return t, nil
}

// Get 按 id 查找 Terminal。
func (r *Registry) Get(id string) (*Terminal, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.terminals[id]
	if !ok {
		return nil, ErrTerminalNotFound
	}
	return t, nil
}

// List 返回全部 Terminal 快照。
func (r *Registry) List() []Snapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Snapshot, 0, len(r.terminals))
	for _, t := range r.terminals {
		out = append(out, t.Snapshot())
	}
	return out
}

// Kill 取消 Terminal 当前命令。
func (r *Registry) Kill(id string) error {
	t, err := r.Get(id)
	if err != nil {
		return err
	}
	t.Kill()
	return nil
}

// Close 取消所有 Terminal。
func (r *Registry) Close() {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, t := range r.terminals {
		t.Kill()
	}
}

func newID() (string, error) {
	return newRandID()
}

// errors
var (
	ErrBusy            = errors.New("terminal: busy")
	ErrTerminalNotFound = errors.New("terminal: not found")
)
