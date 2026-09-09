# LapseCam · 让摄像头替你拍延时，也替你盯着 3D 打印

> 一个 Go + FFmpeg 的轻量服务：添加任意 RTSP 摄像头 → 定时抽帧 → 自动合成 H.264 MP4。
> 除了延时摄影，它还内置了 **AI 打印监控**：配合 Home Assistant，在拓竹（Bambu Lab）等打印机
> 逐层截图，把最近几层画面交给视觉模型判断，炒面 / 堵头 / 打印件被拖走时，第一时间 Bark / Webhook 叫醒你。

单二进制 + SQLite，Docker 或 Armbian 小盒子都能跑。

---

## ✨ 功能亮点

- 🛰️ **AI 打印监控（Vision Monitor）**：逐层截图后自动把最近 N 张帧发给 OpenAI 兼容视觉模型（默认 DeepSeek），识别炒面 / 堵头 / 打印件被拖走 / 积料 / 喷嘴碰撞；连续多次判异常才告警（防误报），支持 Bark（可自建服务）+ Webhook 通知
- 🖼️ **告警现场图上传图床**：确认故障时把现场图上传到自建图床（CloudFlare-ImgBed / S3 / Telegram / WebDAV），Bark 推送带图、Webhook 给公开 URL——手机长按通知即可看现场
- **为 Home Assistant 而生**：固定 URL 的快捷录制接口，自动化里写死即可，重复调用幂等；打印开始即录、结束/暂停即停并出片
- **拓竹 A1 逐层截图**：床滑式打印机专用，`layer`（每层抓一帧）/ `timestamp`（记录层时刻选帧）两种按层模式，成片不再左右横跳
- **日常延时摄影**：秒级抽帧、可设开始/结束时间，到点自动拍、结束自动出片；也能让 HA 按日出日落每天自动生成一条当日延时
- **任意 RTSP 摄像头**：只填 RTSP 地址即可接入；Web 后台一键测试连接（分辨率/编码/帧率）
- **Web 实时预览**：go2rtc 把 RTSP 实时转成浏览器可播的 MSE（延迟约 1~2 秒），按需拉流、断开自动释放
- **断线不丢帧 / 重启自恢复**：RTSP 断线自动重连（5s→10s→30s→60s 退避），已拍帧完整保留；重启后 running 任务继续抽帧、stopping 任务完成编码收尾
- **自动出片**：抽帧与成片解耦，任务结束自动用 x264 压成 H.264 MP4，浏览器直接播放/下载
- **数据清理**：出片后自动删中间帧，一键/定时清理旧视频与无主残留
- **轻量易部署**：Docker 一键起，或 Armbian（树莓派/电视盒子）一键装成 systemd 服务

---


## 📸 界面预览

> 截图来自内置 Web 管理后台（单文件，随二进制分发，无需构建）。

| 摄像头管理 | 延时摄影任务 |
| --- | --- |
| ![摄像头管理](https://raw.githubusercontent.com/zhf883680/LapseCam/master/img/ScreenShot_2026-09-08_165452_462.png) | ![延时摄影任务](https://raw.githubusercontent.com/zhf883680/LapseCam/master/img/ScreenShot_2026-09-08_165518_252.png) |

| 生成的视频 | AI 打印监控 |
| --- | --- |
| ![生成的视频](https://raw.githubusercontent.com/zhf883680/LapseCam/master/img/ScreenShot_2026-09-08_165526_678.png) | ![AI 打印监控](https://raw.githubusercontent.com/zhf883680/LapseCam/master/img/ScreenShot_2026-09-08_165535_313.png) |
## 🚀 快速开始

### 方式一：Docker（直接使用 Docker Hub 镜像）

```bash
docker run -d --name lapsecam \
  --restart unless-stopped \
  -p 8080:8080 \
  -v lapsecam-data:/app/data \
  -e TZ=Asia/Shanghai \
  zhf883680/lapsecam:latest
```

也可以用项目自带的 `docker-compose.yml` 一键启动（已指向上面的镜像）：

```bash
docker compose up -d
```

- 镜像支持多架构：`amd64` / `arm64` / `arm/v7`，Docker 自动按机器架构拉取
- 数据持久化在 named volume `lapsecam-data`（compose 时在宿主机 `./data`）
- 默认使用镜像内置配置；如需改配置，把含 `config.yaml` 的目录挂到 `/app/config`
- Web 后台：`http://<IP>:8080`；健康检查：`GET /api/health`

### 方式二：ARM 设备（Armbian，树莓派/电视盒子）

```bash
sudo bash deploy/install.sh
```

自动检测 arm64/arm 架构、安装 ffmpeg、注册为 systemd 服务。装完访问 `http://<设备IP>:19090`，日志 `journalctl -u lapsecam -f`。生产配置见 `config/config.arm.yaml`（预设适配 ARM 弱 CPU）。

### 数据目录（`/app/data`）

首次启动自动初始化，无需手动创建。运行后生成：

- `database.db`：SQLite（摄像头、任务、视频、AI 分析记录）
- `frames/task-{id}/`：抽帧中间图
- `videos/task-{id}/`：成片 MP4
- `vision/task-{id}/events/`：AI 判异常时留存的现场图
- `logs/task-{id}.log`、`lapsecam.log`：任务日志与服务主日志

> 出片成功后中间帧自动清理（`cleanup.removeFramesAfterEncode`）；旧视频/孤儿数据按 `cleanup` 段定时清理。
> 挂载数据目录只是为了持久化：容器删掉重建后记录和成片都还在。

### 发布与 Docker 镜像（GitHub Actions）

推 `v*` 标签自动发布（`.github/workflows/release.yml`）：

```bash
git tag v1.2.3
git push origin v1.2.3
```

自动完成：交叉编译 Linux 静态二进制（`amd64`/`arm64`/`armv7`，作为 Release 附件）→ 构建并推送多架构 Docker 镜像到 Docker Hub（`zhf883680/lapsecam`）。
> 首次需在仓库 **Settings → Secrets and variables → Actions** 添加 `DOCKERHUB_TOKEN`（Docker Hub Access Token，权限 Read/Write/Delete）。

### 本地开发

需要 Go 1.25+ 与 ffmpeg（含 libx264）：

```bash
go run ./cmd/server
go test ./...
```

---

## 🛰️ AI 打印监控（Vision Monitor）

**核心思路：不需要每几秒让 AI 盯一次。** 由 Home Assistant 在“每一层打完”时调一次截图接口，
LapseCam 把该打印任务**最近 N 张（默认 5 张 = 最近 5 层）帧一次性打包发给视觉模型**，
让模型对比这几张图给出整体结论——打印件有没有被拖走、丝有没有乱掉，前后对比比单张更准。
如果层打得很快（比如 5s 一层），可用 `analyzeIntervalSeconds` 限流：截图照常存，但 AI 分析
最多每 N 秒一次，避免请求过密（默认 30s，`0` 表示每次截图都分析）。

```
HA：检测到打印层变化
   │  POST /api/quick/snapshot?layer=N   （每层一帧，同层自动去重）
   ▼
ffmpeg 抓一帧 → 存入本次打印的帧序列
   │
   ▼（后台自动，不阻塞截图响应）
取最近 vision.analyzeFrames 张帧 → 一次性发给 OpenAI 兼容视觉模型（默认 DeepSeek）
   │
   ▼
normal / spaghetti(炒面) / clog(堵头) / object_displaced(被拖走)
       / nozzle_collision(撞件) / material_buildup(积料) / unknown
   │
   ▼ 规则判定（防误报）
异常 = AI 状态属于异常集合 且 置信度 ≥ minConfidence(0.8)
连续 failureStreak(3) 次判异常 → 确认故障（同一故障带冷却，不轰炸）
   │
   ├── 📲 Bark 推送（支持自建服务，可带现场图 URL）
   └── 🔔 Webhook → Home Assistant → 暂停打印机 / 手机通知
```

### 能检测什么

| 状态 | 含义 | 摄像头好不好判断 |
| --- | --- | --- |
| `normal` | 正常 | — |
| `spaghetti` | 炒面：挤出丝乱成一团不再成型 | ⭐ 很容易 |
| `material_buildup` | 喷嘴周围明显积料 | ⭐⭐ |
| `object_displaced` | **打印件/支撑脱离原位、被喷嘴拖着跑** | ⭐⭐（多图对比很有效） |
| `clog` | 堵头：喷嘴堵塞/严重积料不再出料 | ⭐⭐⭐⭐⭐（很难，只能当参考） |
| `nozzle_collision` | 喷嘴撞到打印件/异物 | ⭐⭐⭐ |
| `unknown` | 看不清/无法判断 | — |

> 你最关心的“打印机拖着一个东西跑，最后堵头”：早期信号其实是 `object_displaced` / 画面剧变，
> 把最近几层一起发给模型，让它对比物体位置，比单看一帧可靠得多。

### 开启步骤

前提：摄像头已接入 LapseCam；打印机使用**逐层截图**流程（见下方 HA 集成 2.5，需切片器开启
Smooth Timelapse）。然后：

**1) 配 AI Key（OpenAI 兼容，默认 DeepSeek）**

Docker 场景推荐环境变量（避免进配置文件）：

```bash
# docker-compose.yml 同目录的 .env
VISION_API_KEY=sk-xxxx
```

或直接写配置 `vision.apiKey`。

**2) 打开配置 `config.yaml`**

```yaml
quick:
  captureMode: "layer"        # 逐层截图模式：帧全部由 /api/quick/snapshot 提供

vision:
  enabled: true               # 打开 AI 分析
  provider: "deepseek"        # 通用 OpenAI 兼容：改 baseUrl+model+apiKey 即可换 OpenAI/OpenRouter…
  baseUrl: "https://api.deepseek.com/v1"
  model: "deepseek-v4-flash-vision-exp"
  timeout: 180s            # qwen 等慢模型可再放宽
  disableThinking: true    # 默认关思考（千问/阿里云生效）

  analyzeFrames: 5            # 每次把最近几张（层）发给模型，按你的打印节奏调
  analyzeIntervalSeconds: 30  # 两次 AI 分析的最小间隔（秒）：层太快时防频繁请求；0=不限
  maxChecksPerTask: 50          # 每个打印任务最多执行多少次 AI 分析（异常通常前期就出现），0=不限
  minConfidence: 0.8          # AI 判异常所需最低置信度
  failureStreak: 3            # 连续 N 次判异常才告警（防误报，可调低到 2 求快）
  cooldownSeconds: 300        # 同一故障重复告警冷却

  bark:                       # 通知方式一（可选）：Bark iOS 推送
    enabled: true
    key: "你的Bark设备key"     # 自建 Bark 就用你服务器里注册的 key
    baseUrl: "https://api.day.app"   # ← 自建 Bark 填你自己的地址，如 http://192.168.1.10:8080
    group: "LapseCam"
    level: "timeSensitive"    # active / timeSensitive / critical
    volume: 10                # 重要警告音量 0-10（level=critical 时生效），0=默认5

  webhook:                    # 通知方式二（可选）：Webhook（HA 等）
    enabled: true
    url: "http://homeassistant:8123/api/webhook/lapsecam-print"

  imageHost:                  # 可选：告警时把现场图上传到自建图床（CloudFlare-ImgBed 的 /upload 接口）
    enabled: true
    provider: "cloudflare-imgbed"
    baseUrl: "https://img.example.com"     # ← 你部署的图床站点，不加结尾斜杠
    apiKey: ""                # 图床 API Token（Bearer）；留空读环境变量 IMAGE_HOST_API_KEY
    authCode: ""              # 上传认证码（与 apiKey 二选一）
    uploadChannel: "cfr2"     # 存储渠道：telegram/cfr2/s3/discord/huggingface/webdav
    uploadFolder: "lapsecam"  # 上传目录（可空）
    returnFormat: "full"      # default（/file/id）| full（完整链接）
```

**3) 重启服务**，然后正常按层截图即可。每截一层图 → 自动分析一次。

### 看结果 / 调试

```bash
curl http://<IP>:19090/api/quick/check                     # 本次打印最近一次分析
curl "http://<IP>:19090/api/quick/checks?limit=10"         # 分析历史
curl http://<IP>:19090/api/quick/checks/23/image           # 某次告警现场图
journalctl -u lapsecam | grep -i printcheck                # 分析/推送日志
```

### 通知说明

- **Bark**：确认故障时推一条通知（标题如「3D 打印异常：炒面」，正文为 AI reason + 置信度）。
  若开启了 `vision.imageHost`，现场图会先上传图床，Bark 推送带 `image`（iOS 长按通知可见）；`baseUrl` 支持自建 Bark 服务。Bark 用法见 <https://bark.day.app/#/tutorial>。
- **现场图留存**：只要某次分析判为异常，就把当时最新一帧复制到 `data/vision/task-{id}/events/`
  （即使还没到告警阈值）。出片/清理会删除 `frames/` 中间帧，但这里的留存副本不受影响，
  「AI 监控」页审计记录里的现场图始终可看。
- **Webhook**：确认故障时 POST JSON：

```json
{
  "taskId": 12,
  "status": "spaghetti",
  "confidence": 0.94,
  "reason": "模型顶部出现大量无规则挤出丝，明显偏离正常打印结构",
  "image": "/api/quick/checks/23/image",
  "timestamp": "2026-09-08T17:00:15+08:00"
}
```

HA 收到后暂停/通知：

```yaml
automation:
  - alias: "打印异常 → 通知并暂停"
    trigger:
      - platform: webhook
        webhook_id: lapsecam-print
    action:
      - service: notify.mobile_app_phone
        data:
          title: "3D 打印可能失败了"
          message: "{{ trigger.json.reason }}"
```

> 提示：模型走 OpenAI 兼容接口，`vision.baseUrl`/`model`/`apiKey` 可指向 OpenAI、DeepSeek、
> OpenRouter 或自建兼容服务。DeepSeek 视觉说明见
> <https://api-docs.deepseek.com/zh-cn/guides/vision>。

---

## 🤖 Home Assistant 集成

LapseCam 给 HA 的接口都是固定 URL、幂等，适合写死在自动化里：

```
POST /api/quick/start             # 开始快捷录制（第一台摄像头）
POST /api/quick/stop              # 停止并出片
POST /api/quick/snapshot?layer=N  # 逐层截图（captureMode=layer）
POST /api/quick/layer?layer=N     # 记录层时刻（captureMode=timestamp）
GET  /api/quick/check             # AI 打印监控最近一次结果
```

### 1. 定义 REST 命令

```yaml
rest_command:
  lapsecam_quick_start:
    url: "http://<LapseCam IP>:19090/api/quick/start"
    method: POST
  lapsecam_quick_stop:
    url: "http://<LapseCam IP>:19090/api/quick/stop"
    method: POST
  lapsecam_quick_snapshot:
    url: "http://<LapseCam IP>:19090/api/quick/snapshot?layer={{ layer }}"
    method: POST
  lapsecam_quick_layer:
    url: "http://<LapseCam IP>:19090/api/quick/layer?layer={{ layer }}"
    method: POST
```

### 2. 拓竹打印机自动录制（定时抽帧版）

```yaml
automation:
  - alias: "3D 打印开始 → 开始延时录制"
    trigger:
      - platform: state
        entity_id: sensor.bambu_lab_status
        to: "printing"
    action:
      - service: rest_command.lapsecam_quick_start

  - alias: "3D 打印结束/暂停 → 停止录制并出片"
    trigger:
      - platform: state
        entity_id: sensor.bambu_lab_status
        from: "printing"
        to: "idle"
      - platform: state
        entity_id: sensor.bambu_lab_status
        from: "printing"
        to: "paused"
    action:
      - service: rest_command.lapsecam_quick_stop
```

打印结束 LapseCam 自动合成 MP4；暂停即出片，恢复打印再 start 会另起一段。

### 2.5 拓竹 A1 逐层截图（床滑式专用 + AI 监控入口）

A1 是床滑式，定时抽帧每帧床 Y 位置随机，成片会左右横跳。两种「按层」模式（`quick.captureMode`），
都要求切片器开启**平滑延时摄影**（Smooth Timelapse），让每层结束工具头在 poop 位停车数秒：

- `layer`：不自动抽帧，每层结束时 HA 调 `/api/quick/snapshot?layer=N` 抓一帧（同层自动去重）。
  **这也是 AI 打印监控的触发入口**：截完一帧自动分析最近 N 张。
- `timestamp`：连续抽帧 + 每层变化时调 `/api/quick/layer?layer=N` 记录时刻，出片时挑最接近每层的帧
  （对触发时机不敏感；`layerOffsetSeconds` 补偿上报与停车偏差，可负，需实测校准）

```yaml
automation:
  - alias: "3D 打印开始 → 开始延时录制"
    trigger:
      - platform: state
        entity_id: sensor.bambu_lab_status
        to: "printing"
    action:
      - service: rest_command.lapsecam_quick_start

  # captureMode=layer：每层变化 → 截图（delay 等工具头在 poop 位停稳，需实测调整）
  - alias: "每层变化 → 延时截图"
    trigger:
      - platform: state
        entity_id: sensor.a1_03900d642904879_current_layer
    condition:
      - condition: template
        value_template: "{{ trigger.to_state.state | int(default=0) > 0 }}"
    action:
      - delay:
          seconds: 2
      - service: rest_command.lapsecam_quick_snapshot
        data:
          layer: "{{ trigger.to_state.state | int }}"

  # captureMode=timestamp：每层变化 → 只记录时刻（不截图），出片时选帧
  # - alias: "每层变化 → 记录层时刻"
  #   trigger:
  #     - platform: state
  #       entity_id: sensor.a1_03900d642904879_current_layer
  #   action:
  #     - service: rest_command.lapsecam_quick_layer
  #       data:
  #         layer: "{{ trigger.to_state.state | int }}"

  - alias: "3D 打印结束/暂停 → 停止录制并出片"
    trigger:
      - platform: state
        entity_id: sensor.bambu_lab_status
        from: "printing"
        to: "idle"
      - platform: state
        entity_id: sensor.bambu_lab_status
        from: "printing"
        to: "paused"
    action:
      - service: rest_command.lapsecam_quick_stop
```

> `layer` 模式配 `quick.captureMode: "layer"`，`timestamp` 配 `"timestamp"`。
> AI 告警通知配置见上文「🛰️ AI 打印监控」。

### 3. 每天日出到日落自动延时

```yaml
automation:
  - alias: "日出 → 开始今日延时"
    trigger:
      - platform: sun
        event: sunrise
    action:
      - service: rest_command.lapsecam_quick_start

  - alias: "日落 → 停止并出片"
    trigger:
      - platform: sun
        event: sunset
    action:
      - service: rest_command.lapsecam_quick_stop
```

> 快捷录制参数（间隔/FPS/分辨率/任务名/抽帧模式）由 `quick` 段控制。完整接口与配置见
> [API 与配置参考](docs/api.md)。

---

## 🖥️ Web 管理后台

浏览器打开 `http://<IP>:<端口>`：

- **摄像头**：增删改查、测试连接、实时预览、在线/离线、启用开关
- **延时任务**：新建（摄像头/间隔/FPS/分辨率/起止时间）、开始/停止、进度与帧数
- **视频**：播放/下载 MP4、删除记录
- **🛰️ AI 监控**：当前打印状态卡片 + 跨任务审计记录（时间/任务/摄像头/状态/置信度/原因/现场图，点击图片可放大）
- **⚙️ 设置**：页面直接改 AI 打印监控配置（抽帧模式、视觉模型、判定策略、Webhook、Bark、图床上传），
  并提供「其他设置」YAML 编辑器覆盖 server/quick/cleanup/preview/编码等所有非 AI 配置，保存并一键重启生效

---

## 🔄 延时摄影工作原理

```
RTSP ─▶ ffmpeg 抽帧（fps=1/间隔）─▶ data/frames/task-{id}/%06d.jpg
                                              │
                              任务结束/手动停止 │
                                              ▼
        ffmpeg 编码（-framerate fps -c:v libx264）─▶ data/videos/task-{id}/*.mp4
```

- 抽帧与成片解耦：RTSP 断线不影响已拍照片，重连后帧号自动接续
- 断线自动重连，退避 `5s → 10s → 30s → 60s`（可配置）
- 容器/服务重启自动恢复：`running` 继续抽帧，`stopping` 完成编码收尾
- 任务状态机：`pending → running → stopping → completed / failed / stopped`

## 🧊 编码太占 CPU 怎么办

1. **限线程**：`ffmpeg.encodeThreads: 2`（0=自动吃满所有核）
2. **更快的预设**：`ffmpeg.encodePreset: "ultrafast"`（比 veryfast 快约一倍，文件略大）
3. **降分辨率**：1080p → 720p，像素少 2.25 倍
4. 实时预览按需拉流，不占用编码线程

> 硬件编码（videotoolbox / v4l2m2m）在路线图中。

---

## 📁 数据目录

```
data/
├── database.db                     # SQLite（摄像头/任务/视频/AI 分析记录）
├── frames/task-{id}/%06d.jpg       # 延时抽帧（layer 模式为每层截图）
├── videos/task-{id}/*.mp4          # 成片
├── vision/task-{id}/events/        # AI 判异常现场图
└── logs/                           # ffmpeg / 监控日志
```

## 📄 文档

- [API 与配置参考](docs/api.md)：全部接口、参数、响应与配置字段

## 🗺️ 路线图（暂未实现）

- ONVIF 自动发现 / 自动获取 RTSP
- 时间水印 / 天气信息
- 硬件编码 / GPU
- 快捷录制指定摄像头 / 多路并发
