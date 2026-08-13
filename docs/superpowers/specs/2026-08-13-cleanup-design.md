# 清理功能设计

日期：2026-08-13
状态：已确认

## 背景

LapseCam 运行一段时间后磁盘占用持续增长：

- `data/frames/task-{id}/` 的中间帧是最大头：一次几小时录制即产生几千张 JPG（几个 GB）；
- `data/videos/task-{id}/*.mp4` 成片长期累积；
- `data/logs/task-{id}.log` 与 `frames/task-{id}.layers.json` 随任务数增长；
- 删除任务/数据库重建等情况会遗留无主目录与文件。

现状只有"删单个任务/单条视频"的手动操作，没有批量或自动清理。目标：提供一键清理 + 可配置的自动清理，安全释放磁盘空间。

## 已确认的决策

- 清理策略放在 `timelapse.Service` 上（它已持有 db + storage + config，且 `Delete`/`DeleteRecord` 已负责删文件），不新增独立包。
- 出片成功（任务状态 `completed`）后才删中间帧；`failed` / `running` / `stopping` / `stopped` / `pending` 一律保留。
- 旧视频保留策略默认关闭（`videoRetentionDays=0`），避免误删用户数据。
- 孤儿清理始终安全：只删数据库里不存在的任务所对应的目录/文件。
- 自动清理随现有 scheduler 循环调度，跑在独立 goroutine 里，不阻塞任务调度。
- 手动清理与自动清理共用一个入口，互斥防并发。

## 配置（新增 cleanup 段）

`config/config.yaml`、`config/config.arm.yaml` 增加，缺省使用代码默认值：

```yaml
cleanup:
  enabled: true                 # 自动清理总开关
  intervalHours: 24             # 自动清理间隔（小时）
  removeFramesAfterEncode: true # 出片成功后删除中间帧（含 selected/ 子目录与 layers 标记）
  videoRetentionDays: 0         # 0=保留全部；>0 只保留最近 N 天的视频记录及文件
  removeOrphans: true           # 清理孤儿数据（无主 task-{id} 目录/日志/标记）
```

代码默认值：`enabled=true`、`intervalHours=24`、`removeFramesAfterEncode=true`、`videoRetentionDays=0`、`removeOrphans=true`。

- `intervalHours<=0` 时回退为默认 24（`applyDefaults` 约束），避免每个 tick 都触发清理；
- `enabled` 只控制**自动**清理；手动 `POST /api/cleanup` 始终执行配置里开启的子策略（`docs/api.md` 需显式说明这一点）。

## API

响应沿用现有 envelope 格式：`{"code":0,"message":"ok","data":...}`。

### POST /api/cleanup

手动触发一次清理。返回本次清理统计：

```json
{
  "cleanedFrames": 2,       // 删除中间帧的任务数
  "deletedVideos": 1,       // 按保留策略删除的视频记录数
  "orphanDirs": 3,          // 删除的孤儿目录数
  "orphanFiles": 5,         // 删除的孤儿文件数
  "freedBytes": 123456      // 释放的磁盘空间（字节）
}
```

- 清理进行中再次调用 → `409`，"清理进行中"。
- 只执行配置里开启的策略（如 `removeFramesAfterEncode=false` 则跳过中间帧清理）。

## 清理规则

### 1. 中间帧清理（removeFramesAfterEncode）

- 查 `timelapse_tasks` 中 status=`completed` 的任务；
- 删除 `frames/task-{id}/`（含 `selected/` 子目录）与 `frames/task-{id}.layers.json`；
- 目录/文件不存在视为成功（幂等）；
- 计数语义：统计中 `cleanedFrames` 只计入"删除前 `frames/task-{id}/` 目录实际存在且被删除"的任务数；删除前先 `os.Stat` 判断目录存在性，不存在则不计数（保证重复清理统计稳定）。`layers.json` 独立于该计数：目录不存在但标记文件存在时，标记文件仍删除，`freedBytes` 计入其字节数；
- 删除后 `enrich` 中 FrameCount 回退为最近一条 success 记录的帧数，避免任务列表帧数显示 0。实现上：`CountFiles` 为 0 且任务已 completed 时，按 `ORDER BY id DESC LIMIT 1` 查询该任务最近一条 status=success 的记录的 frame_count 展示；无 success 记录时展示 0。`timelapse_tasks` 表不存帧数列，回退数据源只有 records 表。

### 2. 旧视频保留（videoRetentionDays）

- `videoRetentionDays <= 0` 时跳过；
- 删除 `created_at < now - retentionDays` 的 `timelapse_records`，逐条删除其 `file_path` 文件后删记录；
- 失败的记录（无文件）同样删除，保持列表干净；
- 删除文件后，对其所在 `videos/task-{id}/` 目录执行 `os.Remove`（仅当目录为空时成功；非空目录报错忽略），顺带清掉空目录，不留残留；
- `deletedVideos` 只计入实际删除的记录条数。

### 3. 孤儿清理（removeOrphans）

- 扫描 `frames/`、`videos/` 根目录下的 `task-{id}` 目录；
- 扫描 `frames/` 根目录下的 `task-{id}.layers.json` 文件与 `logs/` 根目录下的 `task-{id}.log` 文件，同样在删除前逐条重查 ID 是否存在，存在则跳过；
- 目录/文件解析不出 task id（格式不符）一律跳过，不做任何删除；
- 计数语义：`orphanDirs`/`orphanFiles` 只计入实际存在的、被删除的项（删除前 stat 判断）；
- **并发安全（防误删活任务）**：不用"一次性快照 ID 集合"的方式。对每个候选 `task-{id}`，删除前**逐条重查** `timelapse_tasks` 是否存在该 ID，存在则跳过。安全性依据：`Create` 总是先插入行、后由 worker 创建目录（`EnsureDir`），且 AUTOINCREMENT 的 ID 不会被复用——因此重查时行存在的目录必属于活任务，绝不删除；重查时行不存在的目录只可能是任务已删除的残留（`Delete` 本身也会删这些目录，重复删无害）。

### 4. 释放空间统计

- 删除前用 `filepath.WalkDir` 统计将被删除目录/文件的字节数，累加到 `freedBytes`。

## 自动清理调度

- `scheduleLoop` 中记录 `lastCleanup` 时间（服务启动时为 `time.Time{}`，即启动后立即首次检查）；
- 每 tick 检查：`cleanup.enabled && now - lastCleanup >= intervalHours` 且无清理进行中 → `go s.Cleanup()` 并在触发时更新 `lastCleanup`；
- 清理进行中时跳过本轮：`lastCleanup` 不更新，下个 tick 若清理已结束且仍到间隔则再试（即"尽快补跑"，不等下个间隔）；
- 手动 `POST /api/cleanup` 成功后把 `lastCleanup` 置为当前时间，避免手动清理后自动清理紧接着又跑一轮；
- 清理在独立 goroutine 执行，通过 `sync.Mutex`（或 atomic bool）防止手动/自动并发重入。

## 实现

### 服务层（internal/timelapse）

- 新增 `Cleanup() (CleanupStats, error)` 方法，返回统计结构；
- 并发控制：新增独立的 `cleanupMu sync.Mutex` 并配合 `TryLock`；已在进行中时返回哨兵错误 `ErrCleanupInProgress`，API 层据此返回 `409`（不阻塞挂起）；
- 自动调度检查到清理进行中时跳过本轮，下次间隔再试；
- 新增 `enrich` 的帧数回退逻辑；
- 调度器接入：`StartScheduler` 初始化 `lastCleanup`，`scheduleLoop` 检查并触发。

### API 层（api/）

- 新增 `POST /api/cleanup` 路由，薄封装调用 `tl.Cleanup()`；进行中返回 `409`。

### Web 层（web/index.html）

- 「视频」tab 工具栏加「🧹 清理」按钮；
- 点击 `confirm` 后 `POST /api/cleanup`，toast 展示统计（释放 X MB、删除 Y 项），完成后刷新视频列表。

### 配置层（config/）

- `Config` 增加 `Cleanup CleanupConfig`，`applyDefaults` 填充默认值。

## 错误处理

- 清理过程中的单文件/目录删除失败只记录日志，不中断整体清理；
- 无任务、无孤儿、无旧视频时返回全 0 统计，`200`。

## 测试

`internal/timelapse/service_test.go` 增加：

- 清理后 completed 任务的 frames 目录被删除，failed 任务的保留；
- 孤儿目录/日志/标记文件被删除，有效任务的保留；
- `videoRetentionDays` 到期记录及其文件被删除，未到期保留；`0` 时全部保留；
- 重复调用（模拟清理中）返回 `ErrCleanupInProgress`，不并发重入；
- 清理统计（freedBytes/计数）正确，且重复清理（无实际删除）统计稳定；
- 清理后 `Get`/`List` 的 completed 任务 FrameCount 回退为最近 success 记录帧数；无 success 记录时显示 0；
- `intervalHours<=0` 被 `applyDefaults` 归一为默认值。

API/Web 为薄封装，不做额外测试。

## 文档更新

- `README.md`：功能亮点加"数据清理"；数据目录说明补充清理行为；
- `docs/api.md`：加 `POST /api/cleanup` 与 `cleanup` 配置段；
- `config/config.yaml`、`config/config.arm.yaml`：加 `cleanup` 段示例。

## 不做的事（YAGNI）

- 不做 Web 后台的自动清理配置页（只读配置文件）。
- 不按数量（只保留最近 N 条）做视频保留，只按天数。
- 不加清理历史/审计记录。
- 不做数据库表结构迁移。
