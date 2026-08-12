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
  "width": 1920,
  "height": 1080,
  "startAt": "2026-08-12T08:00:00+08:00",
  "endAt": "2026-08-12T18:00:00+08:00"
}
```

时间格式兼容 `RFC3339` / `2006-01-02 15:04:05` / `2006-01-02` 等，不带时区按服务器本地时区解释；`endAt` 可空，为空则手动停止时出片。

任务状态：`pending → running → stopping → completed / failed / stopped`

## 快捷录制（Home Assistant 专用）

两个固定 URL，专门给 HA 等外部自动化调用：无需管理任务 ID、重复调用幂等。

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| POST | `/api/quick/start` | 开始录制（使用第一台摄像头） |
| POST | `/api/quick/stop` | 停止录制并自动出片 |

- 已在录制时 start → `200`，返回当前 `taskId` 与 `"message": "已在录制"`
- 没有录制时 stop → `200`，返回 `"message": "当前没有正在录制的快捷任务"`
- 没有可用摄像头时 start → `400` "没有可用摄像头"
- 同一时间只允许一个快捷录制任务

curl 示例：

```bash
curl -X POST http://192.168.1.20:19090/api/quick/start
curl -X POST http://192.168.1.20:19090/api/quick/stop
```

## 视频

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/videos` | 视频记录列表 |
| GET | `/api/videos/{id}` | 记录详情 |
| GET | `/api/videos/{id}/file` | 播放/下载 MP4（支持 Range） |
| DELETE | `/api/videos/{id}` | 删除记录及文件 |

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
| `ffmpeg.encodePreset/encodeCRF` | x264 预设与质量（CRF 越小越清晰） |
| `scheduler.tickSeconds` | 任务调度轮询间隔 |
| `scheduler.cameraCheckSeconds` | 摄像头在线状态轮询间隔，`0` 关闭 |
| `quick.name/intervalSeconds/outputFps/width/height` | 快捷录制参数，默认 5s/30FPS/1920×1080 |

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
  encodePreset: "medium"
  encodeCRF: 20

scheduler:
  tickSeconds: 1
  cameraCheckSeconds: 60

quick:
  name: "快捷录制"
  intervalSeconds: 5
  outputFps: 30
  width: 1920
  height: 1080
```

## 项目结构

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
├── deploy/                    # Armbian 一键安装脚本 + systemd 服务
├── Dockerfile
└── docker-compose.yml
```
