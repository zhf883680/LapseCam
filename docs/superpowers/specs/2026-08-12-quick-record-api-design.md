# 快捷录制 API 设计（Home Assistant 集成）

日期：2026-08-12
状态：已确认

## 背景

用户使用 Home Assistant（HA）自动化联动 Bambu Lab A1 3D 打印机：打印开始时开始延时录制，打印结束/暂停时停止录制并出片。现有 `/api/timelapse/{id}/start`、`/api/timelapse/{id}/stop` 需要先创建任务并管理任务 ID，且任务停止后不可复用，不适合 HA 直接调用。

目标：提供两个固定 URL 的"快捷录制"接口，HA 自动化里写死 URL 即可，无需管理任务 ID。

## 已确认的决策

- 使用第一台摄像头（`cameras` 表中 id 最小的），不在请求中传参。
- 不加鉴权，局域网内直接 POST 调用。
- 复用现有任务/抽帧/出片机制（方案 1），不做独立模块。
- 重复调用幂等：已在录制时 start 返回成功；没有录制时 stop 返回成功。

## 配置（可选）

`config.yaml` / `config.arm.yaml` 增加 `quick` 段，缺省使用代码默认值：

```yaml
quick:
  name: "快捷录制"      # 识别快捷任务的保留名称
  intervalSeconds: 5    # 抽帧间隔（秒）
  outputFps: 30         # 成片帧率
  width: 1920           # 成片宽
  height: 1080          # 成片高
```

代码默认值：`name="快捷录制"`、`intervalSeconds=5`、`outputFps=30`、`width=1920`、`height=1080`。

## API

响应沿用现有 envelope 格式：`{"code":0,"message":"ok","data":...}`。

### POST /api/quick/start

开始快捷录制。

- 查询 `cameras` 表 id 最小的摄像头；没有摄像头 → `400`，"没有可用摄像头"。
- 存在 status 为 `running` 或 `stopping` 且 name 为快捷任务名的任务 → `200`，幂等返回：
  `{"taskId": N, "status": "running|stopping", "message": "已在录制"}`
- 否则创建任务：`name=快捷任务名`、`cameraId=第一台摄像头`、`startAt=now`、`endAt=null`、
  间隔/FPS/分辨率取配置。创建后立即开始（现有 `Create` 在开始时间已到时自动启动）。
- 成功 → `200`：`{"taskId": N, "status": "running"}`。

### POST /api/quick/stop

停止当前快捷录制并出片。

- 查找 status 为 `running` 或 `stopping` 且 name 为快捷任务名的任务。
- 找到 → 调用现有 `Stop(id)`（异步：停抽帧 → 合成 MP4）→ `200`：`{"taskId": N, "status": "stopping"}`。
- 没有 → `200` 幂等：`{"message": "当前没有正在录制的快捷任务"}`。

## 实现

### 服务层（internal/timelapse）

- 新增 `QuickStart()` / `QuickStop()` 方法。
- 内部互斥锁串行化 start/stop，避免并发竞争（同时 start 创建两个任务、stop 时任务状态已变等）。
- 快捷任务通过保留名称识别（如"快捷录制"），不加数据库字段、不做迁移。
- 启动时互斥锁放在 `Service` 上，与现有 `mu` 区分或复用，实现时保证不与其他方法死锁。
- 重启恢复不受影响：`resumeRunning` 恢复 `running` 任务，名称持久化在库里，`quick/stop` 仍能找到。

### API 层（api/）

- 新增 `POST /api/quick/start`、`POST /api/quick/stop` 两个路由，薄封装调用服务层。

### 配置层（config/）

- `Config` 增加 `Quick QuickConfig`，`applyDefaults` 填充默认值。

## 错误处理

- 无摄像头：`400`，明确错误信息。
- 创建任务失败：`500`，返回错误信息。
- 重复 start / 无录制 stop：幂等 `200`，不报错。

## 测试

`internal/timelapse/service_test.go` 增加：

- 无摄像头时 QuickStart 返回错误。
- 正常 QuickStart 后任务进入 running，返回 taskId。
- 已录制时再次 QuickStart 幂等，返回同一 taskId。
- 正常 QuickStop 后任务进入 stopping。
- 没有录制时 QuickStop 幂等不报错。

API 层为薄封装，不做额外测试。

## HA 侧使用示例（实现后提供）

- 打印开始自动化：`rest_command` 或 `curl` POST `/api/quick/start`。
- 打印结束/暂停自动化：POST `/api/quick/stop`。

## 不做的事（YAGNI）

- 不加鉴权/token。
- 不支持多路并发快捷录制（同一时间只允许一个）。
- 不在请求里传摄像头 ID / 录制参数。
- 不在 Web 后台加"快捷录制"按钮。
