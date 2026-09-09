# LapseCam · 让摄像头替你拍延时，也替你盯着 3D 打印

> Go + FFmpeg 的轻量服务：添加任意 RTSP 摄像头 → 定时抽帧 → 自动合成 H.264 MP4。
> 除了延时摄影，内置 **AI 打印监控**：配合 Home Assistant 在拓竹（Bambu Lab）等打印机逐层截图，
> 把最近几层画面交给视觉模型判断，炒面 / 堵头 / 打印件被拖走时第一时间 **Bark / Webhook** 叫醒你。
> 单二进制 + SQLite，Docker 或 Armbian 小盒子都能跑。

## 功能特性

- 🛰️ **AI 打印监控**：逐层截图后自动把最近 N 张帧发给 OpenAI 兼容视觉模型（默认 DeepSeek，可换阿里云千问 qwen 等），识别炒面 / 堵头 / 打印件被拖走；连续多次判异常才告警（防误报），支持 **Bark（支持自建服务）+ Webhook**
- 🖼️ **告警现场图上传图床**：确认故障时把现场图上传到自建图床（CloudFlare-ImgBed / S3 / Telegram / WebDAV），Bark 推送带图、Webhook 给公开 URL
- 🤖 **为 Home Assistant 而生**：固定 URL 的快捷录制接口，重复调用幂等；打印开始即录、结束/暂停即停并出片
- 🖨️ **拓竹 A1 逐层截图**：床滑式专用，`layer`（每层抓一帧）/ `timestamp`（记录层时刻选帧）两种按层模式，成片不再左右横跳
- 🌅 **日常延时摄影**：秒级抽帧、可设开始/结束时间，到点自动拍、结束自动出片
- 📷 **任意 RTSP 摄像头**：只填 RTSP 地址即可接入；Web 后台一键测试连接
- 👁️ **Web 实时预览**：go2rtc 把 RTSP 实时转成浏览器可播的 MSE（延迟约 1~2 秒），按需拉流
- 🔁 **断线不丢帧 / 重启自恢复**、🎬 **自动出片**（x264）、🧹 **数据清理**

## 界面预览

| 摄像头管理 | 延时摄影任务 |
| --- | --- |
| ![摄像头管理](https://raw.githubusercontent.com/zhf883680/LapseCam/master/img/ScreenShot_2026-09-08_165452_462.png) | ![延时摄影任务](https://raw.githubusercontent.com/zhf883680/LapseCam/master/img/ScreenShot_2026-09-08_165518_252.png) |

| 生成的视频 | AI 打印监控 |
| --- | --- |
| ![生成的视频](https://raw.githubusercontent.com/zhf883680/LapseCam/master/img/ScreenShot_2026-09-08_165526_678.png) | ![AI 打印监控](https://raw.githubusercontent.com/zhf883680/LapseCam/master/img/ScreenShot_2026-09-08_165535_313.png) |

## 快速开始

```bash
docker run -d --name lapsecam \
  --restart unless-stopped \
  -p 8080:8080 \
  -v lapsecam-data:/app/data \
  -e TZ=Asia/Shanghai \
  zhf883680/lapsecam:latest
```

或使用项目自带 `docker-compose.yml`：

```bash
docker compose up -d
```

- 多架构：`amd64` / `arm64` / `arm/v7`
- Web 后台：`http://<IP>:8080`；健康检查：`GET /api/health`

> 如需修改配置，把含 `config.yaml` 的目录挂载到 `/app/config`；数据持久化在 `/app/data`。

## AI 打印监控（Vision Monitor）

不需要每几秒让 AI 盯一次：由 Home Assistant 在每层打完时调一次截图接口，
LapseCam 把该打印任务**最近 N 张帧**一次性发给视觉模型判断，自动对比多张图。

默认配置（`config.yaml`）：

```yaml
quick:
  captureMode: "layer"          # 逐层截图模式

vision:
  enabled: true
  provider: "deepseek"          # OpenAI 兼容：可填阿里云百炼 qwen 等
  baseUrl: "https://api.deepseek.com/v1"   # 如 https://dashscope.aliyuncs.com/compatible-mode/v1
  apiKey: "${VISION_API_KEY}"
  model: "deepseek-v4-flash-vision-exp"
  analyzeFrames: 5              # 每次取最近几张（层）
  analyzeIntervalSeconds: 30    # 两次分析最小间隔（层太快时防频繁请求）
  maxChecksPerTask: 50          # 每个打印任务最多分析次数（异常通常前期就出现），0=不限
  maxImageWidth: 640            # 发给 AI 前缩到该宽度（0=不压缩，省 token）
  aiEnabledByDefault: false     # 新建任务默认关 AI 检测，需在 AI 监控页对任务开启
  failureStreak: 3              # 连续 3 次判异常才告警
  disableThinking: true         # 关闭思考模式（千问/阿里云生效）
  bark:
    enabled: true
    key: "你的Bark设备key"
    baseUrl: "https://api.day.app"   # 自建 Bark 填你自己的地址
  webhook:
    enabled: true
    url: "http://homeassistant:8123/api/webhook/lapsecam-print"
  imageHost:                # 可选：告警时把现场图上传到自建图床
    enabled: true
    baseUrl: "https://img.example.com"   # 图床站点，不加结尾斜杠
    apiKey: ""              # API Token（Bearer）；留空读 IMAGE_HOST_API_KEY
    uploadChannel: "cfr2"   # telegram/cfr2/s3/discord/huggingface/webdav
    uploadFolder: "lapsecam"
```

- HA 每层变化调用 `POST /api/quick/snapshot?layer=N` → 自动截图并分析最近 N 张
- 判异常的现场图自动留存（`data/vision/…`，出片删中间帧不影响）
- 告警支持 **Bark 推送**（可自建服务，开启图床后带现场图）与 **Webhook**（Home Assistant）
- 审计记录可在 Web 后台「🛰️ AI 监控」查看；配置可在「⚙️ 设置」页直接改

> 详细接口与配置见项目 [API 与配置参考](https://github.com/zhf883680/LapseCam/blob/master/docs/api.md)。

## 数据目录（`/app/data`）

```
database.db                  # SQLite（摄像头/任务/视频/AI 分析记录）
frames/task-{id}/            # 抽帧中间图（layer 模式为每层截图）
videos/task-{id}/            # 成片 MP4
vision/task-{id}/events/     # AI 判异常现场图
logs/                        # 任务/监控日志
```

## 支持与文档

- 完整 README / 接口文档：https://github.com/zhf883680/LapseCam
- DeepSeek 视觉模型：https://api-docs.deepseek.com/zh-cn/guides/vision
- Bark 推送：https://bark.day.app/#/tutorial
