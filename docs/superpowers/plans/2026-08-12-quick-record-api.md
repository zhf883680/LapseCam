# 快捷录制 API 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为 Home Assistant 提供两个固定 URL 接口（`POST /api/quick/start`、`POST /api/quick/stop`），实现"触发即录、结束即停并出片"。

**Architecture:** 复用现有任务/抽帧/出片机制。服务层在 `internal/timelapse` 新增 `QuickStart`/`QuickStop`，通过保留任务名称（默认"快捷录制"）识别快捷任务，用包级互斥锁串行化防并发；API 层新增两个薄封装路由；配置层新增可选 `quick` 段。

**Tech Stack:** Go 1.26、modernc SQLite、net/http（Go 1.22+ 方法路由）。

---

## 文件结构

| 文件 | 动作 | 职责 |
| --- | --- | --- |
| `config/config.go` | 修改 | 新增 `QuickConfig` 及默认值 |
| `config/config.yaml` | 修改 | 示例配置加 `quick` 段 |
| `config/config.arm.yaml` | 修改 | ARM 生产配置加 `quick` 段 |
| `internal/timelapse/quick.go` | 新增 | `QuickStart`/`QuickStop`/`findActiveQuickTask`/`ErrNoCamera` |
| `internal/timelapse/quick_test.go` | 新增 | 服务层单元测试 |
| `api/api.go` | 修改 | 注册两个新路由 |
| `api/quick.go` | 新增 | 两个 handler（薄封装） |
| `README.md` | 修改 | API 文档 + HA 使用示例 |

> 注意：`internal/timelapse/service.go` 当前有未提交的改动（List 死锁修复），本计划不修改该文件、不将其纳入提交；`quick.go` 用包级互斥锁避免改动 Service 结构体。

---

### Task 1: 配置层支持 quick 段

**Files:**
- Modify: `config/config.go`
- Modify: `config/config.yaml`
- Modify: `config/config.arm.yaml`

- [ ] **Step 1: 在 Config 结构体中新增 QuickConfig 字段**

在 `config/config.go` 的 `Config` 结构体中增加 `Quick QuickConfig` 字段，并在文件内新增结构体：

```go
type Config struct {
	Server    ServerConfig    `yaml:"server"`
	Database  DatabaseConfig  `yaml:"database"`
	Storage   StorageConfig   `yaml:"storage"`
	FFmpeg    FFmpegConfig    `yaml:"ffmpeg"`
	Scheduler SchedulerConfig `yaml:"scheduler"`
	Quick     QuickConfig     `yaml:"quick"`
}

// QuickConfig 快捷录制配置（POST /api/quick/start 与 /api/quick/stop）。
type QuickConfig struct {
	Name            string `yaml:"name"`            // 快捷任务名称（用于识别）
	IntervalSeconds int    `yaml:"intervalSeconds"` // 抽帧间隔（秒）
	OutputFPS       int    `yaml:"outputFps"`       // 成片帧率
	Width           int    `yaml:"width"`           // 成片宽
	Height          int    `yaml:"height"`          // 成片高
}
```

- [ ] **Step 2: Default() 增加默认值**

在 `config/config.go` 的 `Default()` 返回的 `Config` 字面量中增加：

```go
		Quick: QuickConfig{
			Name:            "快捷录制",
			IntervalSeconds: 5,
			OutputFPS:       30,
			Width:           1920,
			Height:          1080,
		},
```

- [ ] **Step 3: applyDefaults() 填充缺省值**

在 `config/config.go` 的 `applyDefaults()` 末尾追加：

```go
	if c.Quick.Name == "" {
		c.Quick.Name = d.Quick.Name
	}
	if c.Quick.IntervalSeconds <= 0 {
		c.Quick.IntervalSeconds = d.Quick.IntervalSeconds
	}
	if c.Quick.OutputFPS <= 0 {
		c.Quick.OutputFPS = d.Quick.OutputFPS
	}
	if c.Quick.Width <= 0 {
		c.Quick.Width = d.Quick.Width
	}
	if c.Quick.Height <= 0 {
		c.Quick.Height = d.Quick.Height
	}
```

- [ ] **Step 4: 两个配置文件加 quick 段**

`config/config.yaml` 末尾追加：

```yaml
quick:
  name: "快捷录制"
  intervalSeconds: 5
  outputFps: 30
  width: 1920
  height: 1080
```

`config/config.arm.yaml` 末尾追加相同内容。

- [ ] **Step 5: 编译验证**

Run: `go build ./...`
Expected: 无输出，退出码 0。

- [ ] **Step 6: 提交**

```bash
git add config/config.go config/config.yaml config/config.arm.yaml
git commit -m "feat: 快捷录制配置段"
```

---

### Task 2: 服务层 QuickStart（无摄像头报错 + 正常创建）

**Files:**
- Create: `internal/timelapse/quick_test.go`
- Create: `internal/timelapse/quick.go`

- [ ] **Step 1: 写失败测试**

新建 `internal/timelapse/quick_test.go`，内容如下（复用 `service_test.go` 里的 `newTestService`）：

```go
package timelapse

import (
	"errors"
	"testing"

	"timelapse/internal/camera"
)

func TestQuickStartNoCamera(t *testing.T) {
	s, cleanup := newTestService(t)
	defer cleanup()

	if _, _, err := s.QuickStart(); !errors.Is(err, ErrNoCamera) {
		t.Fatalf("expected ErrNoCamera, got %v", err)
	}
}

func TestQuickStartCreatesRunningTask(t *testing.T) {
	s, cleanup := newTestService(t)
	defer cleanup()

	cam, err := s.cam.Create(camera.CameraInput{Name: "cam1", RtspURL: "rtsp://127.0.0.1:1/stream"})
	if err != nil {
		t.Fatal(err)
	}

	task, already, err := s.QuickStart()
	if err != nil {
		t.Fatal(err)
	}
	if already {
		t.Fatal("first QuickStart should not be 'already recording'")
	}
	if task.ID == 0 {
		t.Fatal("expected non-zero task id")
	}
	if task.CameraID != cam.ID {
		t.Fatalf("expected camera %d, got %d", cam.ID, task.CameraID)
	}
	if task.Status != StatusRunning {
		t.Fatalf("expected running, got %s", task.Status)
	}
	if task.Name != "快捷录制" {
		t.Fatalf("expected default name 快捷录制, got %s", task.Name)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/timelapse/ -run TestQuickStart -v`
Expected: 编译失败，`undefined: QuickStart`。

- [ ] **Step 3: 最小实现**

新建 `internal/timelapse/quick.go`：

```go
package timelapse

import (
	"errors"
	"sync"
	"time"
)

// quickMu 串行化快捷录制 start/stop，避免并发创建两个任务或状态竞争。
// 生产环境单服务实例，包级锁等价于实例级锁；放在这里避免改动 Service 结构体。
var quickMu sync.Mutex

// ErrNoCamera 没有可用摄像头时返回。
var ErrNoCamera = errors.New("没有可用摄像头")

// QuickStart 开始快捷录制：使用第一台摄像头立即创建录制任务。
// 第二个返回值标记是否此前已在录制（幂等）。
func (s *Service) QuickStart() (Task, bool, error) {
	quickMu.Lock()
	defer quickMu.Unlock()

	cams, err := s.cam.List()
	if err != nil {
		return Task{}, false, err
	}
	if len(cams) == 0 {
		return Task{}, false, ErrNoCamera
	}

	in := TaskInput{
		Name:            s.cfg.Quick.Name,
		CameraID:        cams[0].ID,
		IntervalSeconds: s.cfg.Quick.IntervalSeconds,
		OutputFPS:       s.cfg.Quick.OutputFPS,
		Width:           s.cfg.Quick.Width,
		Height:          s.cfg.Quick.Height,
		StartAt:         time.Now().Format(time.RFC3339),
	}
	t, err := s.Create(in)
	if err != nil {
		return Task{}, false, err
	}
	return t, false, nil
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/timelapse/ -run TestQuickStart -v`
Expected: `PASS`，两个用例全过。

- [ ] **Step 5: 提交**

```bash
git add internal/timelapse/quick.go internal/timelapse/quick_test.go
git commit -m "feat: 快捷录制 QuickStart 服务"
```

---

### Task 3: QuickStart 幂等

**Files:**
- Modify: `internal/timelapse/quick_test.go`
- Modify: `internal/timelapse/quick.go`

- [ ] **Step 1: 写失败测试**

在 `internal/timelapse/quick_test.go` 末尾追加：

```go
func TestQuickStartIdempotent(t *testing.T) {
	s, cleanup := newTestService(t)
	defer cleanup()

	if _, err := s.cam.Create(camera.CameraInput{Name: "cam1", RtspURL: "rtsp://127.0.0.1:1/stream"}); err != nil {
		t.Fatal(err)
	}

	first, already, err := s.QuickStart()
	if err != nil {
		t.Fatal(err)
	}
	if already {
		t.Fatal("first QuickStart should not be 'already recording'")
	}

	second, already2, err := s.QuickStart()
	if err != nil {
		t.Fatal(err)
	}
	if !already2 {
		t.Fatal("second QuickStart should report already recording")
	}
	if second.ID != first.ID {
		t.Fatalf("expected same task id %d, got %d", first.ID, second.ID)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/timelapse/ -run TestQuickStartIdempotent -v`
Expected: FAIL（第二次调用创建了新任务，ID 不同且 already2=false）。

- [ ] **Step 3: 实现幂等检查**

在 `internal/timelapse/quick.go` 中：

1. 在 `QuickStart()` 的 `quickMu.Lock()` 之后、`s.cam.List()` 之前插入：

```go
	if t, err := s.findActiveQuickTask(s.cfg.Quick.Name); err != nil {
		return Task{}, false, err
	} else if t != nil {
		return *t, true, nil
	}
```

2. 在文件末尾追加 `findActiveQuickTask`：

```go
// findActiveQuickTask 查找 name 指定的 running/stopping 任务，没有则返回 nil。
func (s *Service) findActiveQuickTask(name string) (*Task, error) {
	row := s.db.QueryRow(`SELECT id, name, camera_id, interval_seconds, output_fps, width, height,
		start_at, end_at, status, error_message, actual_started_at, created_at, updated_at
		FROM timelapse_tasks WHERE name=? AND status IN (?, ?) ORDER BY id DESC LIMIT 1`,
		name, StatusRunning, StatusStopping)
	var t Task
	err := scanTask(row, &t)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}
```

3. `import` 增加 `"database/sql"`。

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/timelapse/ -run TestQuickStart -v`
Expected: PASS，三个用例全过。

- [ ] **Step 5: 提交**

```bash
git add internal/timelapse/quick.go internal/timelapse/quick_test.go
git commit -m "feat: 快捷录制 start 幂等"
```

---

### Task 4: 服务层 QuickStop

**Files:**
- Modify: `internal/timelapse/quick_test.go`
- Modify: `internal/timelapse/quick.go`

- [ ] **Step 1: 写失败测试**

在 `internal/timelapse/quick_test.go` 末尾追加：

```go
func TestQuickStop(t *testing.T) {
	s, cleanup := newTestService(t)
	defer cleanup()

	if _, err := s.cam.Create(camera.CameraInput{Name: "cam1", RtspURL: "rtsp://127.0.0.1:1/stream"}); err != nil {
		t.Fatal(err)
	}
	task, _, err := s.QuickStart()
	if err != nil {
		t.Fatal(err)
	}

	stopped, found, err := s.QuickStop()
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("expected found=true")
	}
	if stopped.ID != task.ID {
		t.Fatalf("expected task %d, got %d", task.ID, stopped.ID)
	}
	switch stopped.Status {
	case StatusStopping, StatusFailed, StatusCompleted:
	default:
		t.Fatalf("unexpected status after stop: %s", stopped.Status)
	}
}

func TestQuickStopNoRecord(t *testing.T) {
	s, cleanup := newTestService(t)
	defer cleanup()

	if _, found, err := s.QuickStop(); err != nil {
		t.Fatal(err)
	} else if found {
		t.Fatal("expected found=false")
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/timelapse/ -run TestQuickStop -v`
Expected: 编译失败，`undefined: QuickStop`。

- [ ] **Step 3: 实现 QuickStop**

在 `internal/timelapse/quick.go` 的 `findActiveQuickTask` 之前插入：

```go
// QuickStop 停止当前正在录制的快捷任务并出片。
// 没有正在录制的快捷任务时返回 found=false（幂等）。
func (s *Service) QuickStop() (Task, bool, error) {
	quickMu.Lock()
	defer quickMu.Unlock()

	t, err := s.findActiveQuickTask(s.cfg.Quick.Name)
	if err != nil {
		return Task{}, false, err
	}
	if t == nil {
		return Task{}, false, nil
	}
	if err := s.Stop(t.ID); err != nil {
		return Task{}, false, err
	}
	cur, err := s.Get(t.ID)
	if err != nil {
		return Task{}, false, err
	}
	return cur, true, nil
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/timelapse/ -run TestQuickStop -v`
Expected: PASS，两个用例全过。

- [ ] **Step 5: 提交**

```bash
git add internal/timelapse/quick.go internal/timelapse/quick_test.go
git commit -m "feat: 快捷录制 QuickStop 服务"
```

---

### Task 5: API 路由与 handler

**Files:**
- Modify: `api/api.go`
- Create: `api/quick.go`

- [ ] **Step 1: 注册路由**

在 `api/api.go` 的 `Handler()` 中，`// 视频` 分组之后、`// 健康检查` 之前插入：

```go
	// 快捷录制（Home Assistant 等外部自动化调用）
	mux.HandleFunc("POST /api/quick/start", s.quickStart)
	mux.HandleFunc("POST /api/quick/stop", s.quickStop)
```

- [ ] **Step 2: 新增 handler**

新建 `api/quick.go`：

```go
package api

import (
	"errors"
	"net/http"

	"timelapse/internal/timelapse"
)

// quickStart 开始快捷录制（供 HA 等外部自动化调用）。
func (s *Server) quickStart(w http.ResponseWriter, r *http.Request) {
	t, already, err := s.tl.QuickStart()
	if err != nil {
		if errors.Is(err, timelapse.ErrNoCamera) {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	msg := "已开始录制"
	if already {
		msg = "已在录制"
	}
	writeJSON(w, http.StatusOK, map[string]any{"taskId": t.ID, "status": t.Status, "message": msg})
}

// quickStop 停止当前快捷录制并出片（幂等）。
func (s *Server) quickStop(w http.ResponseWriter, r *http.Request) {
	t, found, err := s.tl.QuickStop()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !found {
		writeJSON(w, http.StatusOK, map[string]any{"message": "当前没有正在录制的快捷任务"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"taskId": t.ID, "status": t.Status})
}
```

- [ ] **Step 3: 编译与全量测试**

Run: `go build ./...`
Expected: 无输出，退出码 0。

Run: `go test ./...`
Expected: 全部 PASS。

- [ ] **Step 4: 提交**

```bash
git add api/api.go api/quick.go
git commit -m "feat: 快捷录制 HTTP 接口"
```

---

### Task 6: README 文档与 HA 示例

**Files:**
- Modify: `README.md`

- [ ] **Step 1: 新增 API 文档**

在 `README.md` 的 `### 视频` 一节之后新增：

```markdown
### 快捷录制（Home Assistant 自动化）

两个固定接口，无需管理任务 ID：

```
POST /api/quick/start    # 开始录制（使用第一台摄像头）
POST /api/quick/stop     # 停止录制并出片
```

重复调用安全：已在录制时 start 返回"已在录制"，没有录制时 stop 返回"当前没有录制"。
录制参数（间隔/FPS/分辨率/任务名）由配置 `quick` 段控制，缺省为 5s/30FPS/1920×1080。
```

- [ ] **Step 2: 新增 HA 使用示例**

接在上一节之后：

```markdown
HA `configuration.yaml` 定义两个 REST 命令：

```yaml
rest_command:
  lapsecam_quick_start:
    url: "http://<Armbian IP>:19090/api/quick/start"
    method: POST
  lapsecam_quick_stop:
    url: "http://<Armbian IP>:19090/api/quick/stop"
    method: POST
```

打印开始自动化里加 `- action: rest_command.lapsecam_quick_start`；
打印结束/暂停自动化里加 `- action: rest_command.lapsecam_quick_stop`。
```

- [ ] **Step 3: 提交**

```bash
git add README.md
git commit -m "docs: 快捷录制接口与 HA 示例"
```

---

### Task 7: 全量验证

**Files:** 无

- [ ] **Step 1: 全量测试**

Run: `go test ./... -count=1`
Expected: 全部 PASS，退出码 0。

- [ ] **Step 2: 构建产物**

Run: `go build -ldflags="-s -w" -o /tmp/lapsecam-quick-check ./cmd/server`
Expected: 无输出，退出码 0。

- [ ] **Step 3: 确认提交历史与工作区**

Run: `git log --oneline -6` 和 `git status --short`
Expected: 本计划 6 个 feat/docs 提交按序存在；`internal/timelapse/service.go` 的既有未提交改动仍在工作区（未被打包进任何提交）。

