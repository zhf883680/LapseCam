# LapseCam · Go + FFmpeg 延时摄影服务

纯后端 Docker 服务：**Go 负责业务与任务管理，FFmpeg 负责 RTSP 拉流 / 抽帧 / H.264 编码**。
第一版不做 ONVIF，用户直接填写 RTSP 地址即可跑通整条链路。

```
Vue / 浏览器 ──HTTP──▶ Go REST API ──▶ FFmpeg ──RTSP/H.264──▶ 摄像头
                              │
                              ├── Camera Manager（配置/在线状态/自动重连）
                              ├── Timelapse Manager（定时任务/状态）
                              ├── FFmpeg Manager（RTSP→JPEG，JPEG→H.264 MP4）
                              ├── Storage Manager（图片/视频/清理）
                              └── SQLite（camera / task / record）
```

## 核心流程

```
RTSP ──▶ ffmpeg 抽帧（fps=1/间隔）──▶ data/frames/task-{id}/%06d.jpg
                                                    │
                                   任务结束/手动停止 │
                                                    ▼
                             ffmpeg 编码（-framerate fps -c:v libx264）──▶ data/videos/task-{id}/*.mp4
```

- 抽帧与成片解耦：RTSP 断线不影响已拍到的照片，重连后帧编号自动接续。
- 断线自动重连，退避序列 `5s → 10s → 30s → 60s`（可配置），连接稳定后重置。
- 容器重启自动恢复：`running` 任务继续抽帧，`stopping` 任务完成编码收尾。

## 目录结构

```
├── cmd/server/main.go         # 入口
├── api/                       # REST 路由
├── internal/
│   ├── camera/                # 摄像头 CRUD / 测试连接 / 在线探测
│   ├── timelapse/             # 任务 CRUD / 调度器 / worker / 视频记录
│   ├── ffmpeg/                # ffmpeg 封装：抽帧、编码、探测
│   ├── storage/               # 数据目录与清理
│   └── database/              # SQLite（modernc 纯 Go 驱动，无 CGO）
├── config/config.yaml         # 配置
├── data/                      # 运行时数据（docker 挂载持久化）
├── Dockerfile
└── docker-compose.yml
```

## 本地运行

需要本机安装 Go 1.25+ 与 FFmpeg（含 libx264）。

```bash
go run ./cmd/server                       # 默认读取 config/config.yaml
go run ./cmd/server -config /path/to.yaml # 或指定配置
go test ./...                             # 运行测试
```

## Docker 运行

```bash
docker compose up -d --build
```

- 数据（视频/帧/数据库）挂载在宿主机 `./data`，容器删了不丢。
- 配置挂载在 `./config`，改完重启容器生效。
- 健康检查：`GET /api/health`。

## 配置说明（config/config.yaml）

| 字段 | 说明 |
| --- | --- |
| `server.addr` | 监听地址，默认 `:8080` |
| `database.path` | SQLite 路径 |
| `storage.baseDir/framesDir/videosDir` | 数据目录 |
| `ffmpeg.probeTimeout` | 摄像头探测超时 |
| `ffmpeg.rtspTransport` | RTSP 传输协议 `tcp`/`udp` |
| `ffmpeg.captureJPEGQuality` | 抽帧 JPEG 质量 2-31，越小越清晰 |
| `ffmpeg.captureBackoff` | 断线重连退避序列 |
| `ffmpeg.encodePreset/encodeCRF` | x264 预设与质量（CRF 越小越清晰） |
| `scheduler.tickSeconds` | 任务调度轮询间隔 |
| `scheduler.cameraCheckSeconds` | 摄像头在线状态轮询间隔，`0` 关闭 |

## API

统一响应格式：`{"code":0,"message":"ok","data":...}`，`code` 非 0 表示错误。

### 摄像头

```
GET    /api/cameras               # 列表（含在线状态，密码脱敏）
POST   /api/cameras               # 新增
PUT    /api/cameras/{id}          # 修改（password 为空表示不改）
DELETE /api/cameras/{id}          # 删除（被进行中的任务引用时返回 409）
POST   /api/cameras/{id}/test     # 测试连接（ffprobe，返回分辨率/编码/帧率）
```

```json
{ "name": "客厅摄像头", "rtspUrl": "rtsp://192.168.1.100:554/stream1", "username": "admin", "password": "******" }
```

### 延时摄影任务

```
GET    /api/timelapse             # 列表（含帧数/进度）
POST   /api/timelapse             # 新建
GET    /api/timelapse/{id}        # 详情
POST   /api/timelapse/{id}/start  # 手动开始
POST   /api/timelapse/{id}/stop   # 手动停止（生成 MP4 后完成）
DELETE /api/timelapse/{id}        # 删除（含视频/帧文件）
```

```json
{
  "name": "植物生长",
  "cameraId": 1,
  "intervalSeconds": 10,
  "outputFps": 30,
  "width": 1920,
  "height": 1080,
  "startAt": "2026-08-12T08:00:00+08:00",
  "endAt": "2026-08-12T18:00:00+08:00"
}
```

> 时间兼容 `RFC3339` / `2006-01-02 15:04:05` / `2006-01-02` 等格式，不带时区按服务器本地时区解释；
> `endAt` 可空，为空则手动停止时出片。

任务状态：`pending → running → stopping → completed / failed / stopped`

### 视频

```
GET    /api/videos                # 视频记录列表
GET    /api/videos/{id}           # 记录详情
GET    /api/videos/{id}/file      # 播放/下载 MP4（支持 Range）
DELETE /api/videos/{id}           # 删除记录及文件
```

## 示例：10 小时 / 每 10 秒 / 30 FPS

```
10 小时 × 60 × 60 ÷ 10 秒 = 3600 帧
3600 ÷ 30 FPS = 120 秒视频
输出：data/videos/task-1/植物生长_2026-08-12.mp4
```

## 数据目录

```
data/
├── database.db                  # SQLite
├── frames/task-{id}/%06d.jpg    # 抽帧图片
├── videos/task-{id}/*.mp4       # 成片
└── logs/task-{id}.log           # ffmpeg 日志
```

## 第二阶段（暂未实现）

- ONVIF 自动发现 / 自动获取 RTSP
- Web 实时预览 / WebSocket
- 时间水印 / 天气信息
- 硬件编码 / GPU
