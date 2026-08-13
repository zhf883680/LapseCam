# LapseCam API 与配置参考

本文档为 LapseCam 的完整接口与配置说明。功能与使用场景（Home Assistant 集成、3D 打印机、日常延时摄影）见 [README](../README.md)。

## 基础约定

- Base URL：`http://<服务器IP>:<端口>`（Docker 默认 `8080`，Armbian 生产配置 `19090`）
- 统一响应格式：`{"code":0,"message":"ok","data":...}`，`code` 非 0 表示错误
- 接口未加鉴权，适合局域网内使用

## 摄像头

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/cameras` | 列表（含在线状态，密码脱敏） |
| POST | `/api/cameras` | 新增 |
| PUT | `/api/cameras/{id}` | 修改（password 留空表示不改） |
| DELETE | `/api/cameras/{id}` | 删除（被进行中任务引用时返回 409） |
| POST | `/api/cameras/{id}/test` | 测试连接（ffprobe，返回分辨率/编码/帧率） |

新增 / 修改请求体：

```json
{ "name": "客厅摄像头", "rtspUrl": "rtsp://192.168.1.100:554/stream1", "username": "admin", "password": "******" }
```

## 延时任务

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/timelapse` | 列表（含帧数/进度） |
| POST | `/api/timelapse` | 新建 |
| GET | `/api/timelapse/{id}` | 详情 |
| POST | `/api/timelapse/{id}/start` | 手动开始 |
| POST | `/api/timelapse/{id}/stop` | 手动停止（生成 MP4 后完成） |
| DELETE | `/api/timelapse/{id}` | 删除（含视频/帧文件） |

新建请求体：

```json
{
  "name": "植物生长",
  "cameraId": 1,
  "intervalSeconds": 10,
  "outputFps": 30,
  "width": 1280,
  "height": 720,
  "startAt": "2026-08-12T08:00:00+08:00",
  "endAt": "2026-08-12T18:00:00+08:00"
}
```

时间格式兼容 `RFC3339` / `2006-01-02 15:04:05` / `2006-01-02` 等，不带时区按服务器本地时区解释；`endAt` 可空，为空则手动停止时出片。

任务状态：`pending → running → stopping → completed / failed / stopped`

## 快捷录制（Home Assistant 专用）

几个固定 URL，专门给 HA 等外部自动化调用：无需管理任务 ID、重复调用幂等。

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| POST | `/api/quick/start` | 开始录制（使用第一台摄像头） |
| POST | `/api/quick/stop` | 停止录制并自动出片 |
| POST | `/api/quick/snapshot?layer=N` | 逐层截图：为当前快捷任务抓一帧（需 `quick.captureMode=layer`） |
| POST | `/api/quick/layer?layer=N` | 记录层变化时间戳（`quick.captureMode=timestamp` 出片选帧用） |

- 已在录制时 start → `200`，返回当前 `taskId` 与 `"message": "已在录制"`
- 没有录制时 stop → `200`，返回 `"message": "当前没有正在录制的快捷任务"`
- 没有可用摄像头时 start → `400` "没有可用摄像头"
- 同一时间只允许一个快捷录制任务

### 三种抽帧模式（`quick.captureMode`）

| 模式 | 行为 | 适用 |
| --- | --- | --- |
| `interval`（默认） | 按 `intervalSeconds` 定时抽帧，全部入片 | 普通场景 |
| `layer` | 不自动抽帧，由外部在每层结束时调 `/api/quick/snapshot` 抓一帧 | 床滑式打印机（拓竹 A1）逐层截图 |
| `timestamp` | 连续抽帧 + 外部调 `/api/quick/layer` 记录每层变化时刻，出片时挑最接近每层的帧 | 层触发时机不精确时的兜底 |

`/api/quick/snapshot` 幂等：带 `layer` 参数时同一层只截一次（防 HA 传感器抖动重复触发）；
`layer` 省略时每次都截。`/api/quick/layer` 只是记录时刻，不抓帧。

> 提示：床滑式打印机（如拓竹 A1）要成片平滑，切片器需开启「平滑延时摄影」（Smooth Timelapse），
> 让每层结束时工具头在 poop 位停车数秒、床停在固定 Y 位置，截图/选帧才落在稳定位置。
> `timestamp` 模式里 `layerOffsetSeconds` 用于补偿「层变化上报时刻」与「实际停车时刻」的偏差
> （层变化上报晚于停车时取负值，需实测校准）。

curl 示例：

```bash
curl -X POST http://192.168.1.20:19090/api/quick/start
# captureMode=layer：每层结束时触发
curl -X POST "http://192.168.1.20:19090/api/quick/snapshot?layer=3"
# captureMode=timestamp：每层变化时触发
curl -X POST "http://192.168.1.20:19090/api/quick/layer?layer=3"
curl -X POST http://192.168.1.20:19090/api/quick/stop
```

## 视频

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/videos` | 视频记录列表 |
| GET | `/api/videos/{id}` | 记录详情 |
| GET | `/api/videos/{id}/file` | 播放/下载 MP4（支持 Range） |
| DELETE | `/api/videos/{id}` | 删除记录及文件 |

## 数据清理

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| POST | `/api/cleanup` | 手动触发一次清理，返回 `{cleanedFrames, deletedVideos, orphanDirs, orphanFiles, freedBytes}`；清理进行中返回 `409` |

- 清理内容（由配置 `cleanup` 段控制）：
  - `removeFramesAfterEncode=true`：删除已出片（completed）任务的中间帧与层标记；
  - `videoRetentionDays>0`：删除 N 天前的视频记录及文件；
  - `removeOrphans=true`：删除数据库里已不存在的任务残留目录/日志/标记。
- `cleanup.enabled` 只控制**自动**清理；手动 `POST /api/cleanup` 始终执行上述开启的策略。

## 实时预览（go2rtc）

Web 后台摄像头列表的「预览」按钮，用 go2rtc 把摄像头 RTSP 实时转成浏览器可播的 MSE 流（延迟约 1~2 秒），不占用抽帧/编码线程。

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/preview` | 预览是否启用：`{"enabled":true,"basePath":"/go2rtc"}` |
| GET | `/go2rtc/*` | go2rtc 反代（播放页、API、WebSocket 都在这个前缀下） |

- 播放页 URL：`/go2rtc/stream.html?src=cam-{id}&mode=mse`（`src` 为摄像头对应的 go2rtc 流名）
- go2rtc 只绑 `127.0.0.1`，不直接对外；LapseCam 反代 `/go2rtc/*` 后保持单端口访问
- 摄像头增删改后自动同步 go2rtc 流；go2rtc 按需拉流——有客户端观看才连摄像头，全部断开后自动释放
- go2rtc 二进制：Docker 镜像已内置；ARM 安装脚本自动下载；本地开发需自行安装（`brew install go2rtc` 或去 [GitHub Releases](https://github.com/AlexxIT/go2rtc/releases) 下载）

## 配置说明（config.yaml）

| 字段 | 说明 |
| --- | --- |
| `server.addr` | 监听地址，默认 `:8080` |
| `database.path` | SQLite 路径 |
| `storage.baseDir/framesDir/videosDir` | 数据目录 |
| `ffmpeg.probeTimeout` | 摄像头探测超时 |
| `ffmpeg.rtspTransport` | RTSP 传输协议 `tcp`/`udp` |
| `ffmpeg.captureJPEGQuality` | 抽帧 JPEG 质量 2-31，越小越清晰 |
| `ffmpeg.captureBackoff` | 断线重连退避序列 |
| `ffmpeg.encodePreset/encodeCRF` | x264 预设与质量（CRF 越小越清晰，建议 26-28） |
| `ffmpeg.encodeMaxRateKbps` | 成片码率上限 kbps，防止画面突变时体积暴涨（0 或缺省用默认值 4000） |
| `ffmpeg.encodeThreads` | 编码线程数，`0` 自动（吃满所有核）；小盒子设 `2-3` 可明显降低 CPU 占用 |
| `preview.enabled` | 是否启用 Web 实时预览（go2rtc），默认 `true` |
| `preview.binary` | go2rtc 可执行文件路径 |
| `preview.addr` | go2rtc 内部监听地址，默认 `127.0.0.1:1984`（只绑本机） |
| `preview.basePath` | 反代前缀，默认 `/go2rtc` |
| `preview.rtspTransport` | 预览拉流传输协议 `tcp`/`udp` |
| `preview.media` | 只取视频轨 `video` |
| `preview.startTimeout` | 启动等待 go2rtc 就绪超时 |
| `scheduler.tickSeconds` | 任务调度轮询间隔 |
| `scheduler.cameraCheckSeconds` | 摄像头在线状态轮询间隔，`0` 关闭 |
| `quick.name` | 快捷任务名称，默认"快捷录制" |
| `quick.captureMode` | 抽帧模式 `interval`/`layer`/`timestamp`，默认 `interval` |
| `quick.intervalSeconds` | 抽帧间隔，默认 5s |
| `quick.layerOffsetSeconds` | timestamp 选帧偏移（秒，可负），默认 0 |
| `quick.layerWindowSeconds` | timestamp 选帧窗口（秒），默认 5 |
| `quick.outputFps/width/height` | 成片参数，默认 30FPS/1280×720 |
| `cleanup.enabled` | 自动清理总开关，默认 `true`（手动清理不受影响） |
| `cleanup.intervalHours` | 自动清理间隔（小时），默认 24，`<=0` 回退 24 |
| `cleanup.removeFramesAfterEncode` | 出片成功后删除中间帧，默认 `true` |
| `cleanup.videoRetentionDays` | 0=保留全部；>0 只保留最近 N 天视频，默认 0 |
| `cleanup.removeOrphans` | 清理孤儿数据，默认 `true` |

完整配置示例（ARM 生产版见 `config/config.arm.yaml`）：

```yaml
server:
  addr: ":8080"

database:
  path: "data/database.db"

storage:
  baseDir: "data"
  framesDir: "frames"
  videosDir: "videos"

ffmpeg:
  binary: "ffmpeg"
  ffprobe: "ffprobe"
  probeTimeout: 8s
  rtspTransport: "tcp"
  captureJPEGQuality: 2
  captureBackoff: ["5s", "10s", "30s", "60s"]
  encodePreset: "veryfast"     # CPU 吃紧用 veryfast/ultrafast；想压得更小用 slow/veryslow
  encodeCRF: 26
  encodeMaxRateKbps: 4000
  encodeThreads: 0            # 0=自动；小盒子可设 2-3 限制 CPU

preview:
  enabled: true
  binary: "go2rtc"
  addr: "127.0.0.1:1984"
  basePath: "/go2rtc"
  rtspTransport: "tcp"
  media: "video"
  startTimeout: 10s

scheduler:
  tickSeconds: 1
  cameraCheckSeconds: 60

quick:
  name: "快捷录制"
  captureMode: "interval"   # interval=定时抽帧(默认) | layer=逐层截图 | timestamp=逐层选帧
  intervalSeconds: 5
  layerOffsetSeconds: 0     # timestamp 模式：选帧目标 = 层变化时刻 + 偏移（秒，可负）
  layerWindowSeconds: 5     # timestamp 模式：选帧窗口（秒）
  outputFps: 30
  width: 1280
  height: 720
```

## 项目结构

```
├── cmd/server/main.go         # 入口
├── api/                       # REST 路由
├── internal/
│   ├── camera/                # 摄像头 CRUD / 测试连接 / 在线探测
│   ├── timelapse/             # 任务 CRUD / 调度器 / worker / 视频记录
│   ├── ffmpeg/                # ffmpeg 封装：抽帧、编码、探测
│   ├── preview/               # go2rtc 子进程：实时预览流管理
│   ├── storage/               # 数据目录与清理
│   └── database/              # SQLite（modernc 纯 Go 驱动，无 CGO）
├── config/config.yaml         # 配置
├── data/                      # 运行时数据（docker 挂载持久化）
├── deploy/                    # Armbian 一键安装脚本 + systemd 服务
├── Dockerfile
└── docker-compose.yml
```
