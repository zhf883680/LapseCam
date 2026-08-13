# LapseCam · 让摄像头替你拍延时摄影

> LapseCam 最初就是为了搭配 **Home Assistant** 给 **拓竹（Bambu Lab）3D 打印机** 做自动延时摄影而生的：
> 打印开始 → 自动开始录制，打印结束/暂停 → 自动停录出片，整场打印浓缩成一段视频。
> 同一套服务也覆盖日常延时摄影：日出日落、云海、植物生长、施工记录……添加摄像头、设好间隔，剩下的交给它。

Go + FFmpeg 构建的轻量延时摄影服务：添加 RTSP 摄像头 → 定时抽帧 → 自动合成 H.264 MP4。单二进制 + SQLite，Docker 或 Armbian 小盒子都能跑。

## ✨ 功能亮点

- **为 Home Assistant 而生**：两个固定 URL 的快捷录制接口，自动化里写死即可，无需管理任务 ID，重复调用幂等安全
- **拓竹打印机自动延时**：打印开始即录、结束即停，自动出片回放整场打印过程
- **日常延时摄影**：秒级抽帧间隔、可设开始/结束时间，到点自动拍、结束自动出片；也能让 HA 按日出日落每天自动生成一条当日延时
- **任意 RTSP 摄像头**：只填 RTSP 地址即可接入，无需 ONVIF；Web 后台一键测试连接（返回分辨率/编码/帧率）
- **断线不丢帧**：RTSP 断线自动重连（5s→10s→30s→60s 退避，可配置），重连后帧号自动接续，已拍帧完整保留
- **重启自恢复**：服务/容器重启后，running 任务继续抽帧，stopping 任务完成编码收尾
- **自动出片**：抽帧与成片解耦，任务结束自动用 x264 压成 H.264 MP4，浏览器直接播放/下载
- **Web 管理后台**：单文件内嵌，摄像头/任务/视频三个面板，实时进度
- **轻量易部署**：Docker 一键起，或 Armbian（树莓派/电视盒子）一键装成 systemd 服务

## 🎬 典型使用场景

### 🖨️ 拓竹（Bambu Lab）打印机自动延时

项目的最初目标场景。Home Assistant 联动打印机状态：打印开始 → LapseCam 开始延时录制；打印结束/暂停 → 自动停录并合成 MP4。不用盯屏幕，打完后直接回放整场打印。

### 🌅 日常延时摄影

日出日落、云海流动、植物生长、装修施工、城市车流……在 Web 后台新建一个任务即可：选摄像头、设抽帧间隔和开始/结束时间，到点自动拍、结束自动出片。

时长换算参考：

```
10 小时 × 3600 ÷ 10 秒/帧 = 3600 帧
3600 帧 ÷ 30 FPS = 120 秒成片
```

### 🏠 家庭摄像头随手录

连接家里任意 RTSP 摄像头（NVR / IPC），随时手动录一段、快速出片，浏览器直接回放。

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

- 镜像支持多架构：`amd64` / `arm64` / `arm/v7`，Docker 会自动按机器架构拉取
- 视频/帧/数据库持久化在 named volume `lapsecam-data`；用 compose 时持久化在宿主机 `./data`
- 默认使用镜像内置配置；如需改配置，可挂载含 `config.yaml` 的目录到 `/app/config`
- Web 后台：`http://<IP>:8080`；健康检查：`GET /api/health`

### 数据目录（`/app/data`）说明

`/app/data` **不用提前创建、也完全可以是空的**，服务第一次启动时会自动初始化。运行后里面会自动生成：

- `database.db`：SQLite 数据库，存摄像头、任务、视频记录
- `frames/task-{id}/`：抽帧的中间图片（合成视频用）
- `videos/task-{id}/`：最终合成的 MP4 成片
- `logs/task-{id}.log`：每个任务的抽帧/编码日志
- `lapsecam.log`：服务主日志

挂载它只是为了**持久化**：容器删掉、重建后，任务记录和成片都还在。如果只是临时试用，也**可以不挂载**，容器删除后数据会一起丢失。

### 方式二：ARM 设备（Armbian，树莓派/电视盒子）

```bash
sudo bash deploy/install.sh
```

自动检测 arm64/arm 架构、自动安装 ffmpeg、注册为 systemd 服务。装完访问 `http://<设备IP>:19090`，查看日志 `journalctl -u lapsecam -f`。生产配置在 `config/config.arm.yaml`（编码预设 `veryfast`，适配 ARM 弱 CPU）。

### 发布与 Docker 镜像（GitHub Actions）

推一个 `v*` 标签即可触发自动发布（`.github/workflows/release.yml`）：

```bash
git tag v1.2.3
git push origin v1.2.3
```

> 首次发布前需先添加一个密钥，否则 Docker 推送会失败：
> 仓库 **Settings → Secrets and variables → Actions → New repository secret**，新增
> `DOCKERHUB_TOKEN`，值为 Docker Hub 的访问令牌（Account Settings → Security → Access Tokens，权限选 Read/Write/Delete）。

GitHub Actions 会自动：

1. 交叉编译 Linux 静态二进制：`amd64` / `arm64` / `armv7`，作为附件上传到 Release；
2. 构建并推送多架构 Docker 镜像到 **Docker Hub**。

Docker Hub 镜像：`zhf883680/lapsecam:latest`（多架构：`amd64` / `arm64` / `arm/v7`），直接用法见上方「方式一：Docker」。

### 本地开发

需要 Go 1.25+ 与 ffmpeg（含 libx264）：

```bash
go run ./cmd/server
go test ./...
```

## 🤖 Home Assistant 集成

LapseCam 提供两个专门给 HA 等外部自动化调用的“快捷录制”接口：

```
POST /api/quick/start   # 开始录制（使用第一台摄像头）
POST /api/quick/stop    # 停止录制并自动出片
```

URL 固定、无需管理任务 ID、重复调用幂等：已在录制时 start 返回“已在录制”，没有录制时 stop 返回“当前没有录制”。

### 1. 定义 REST 命令

在 HA 的 `configuration.yaml` 中：

```yaml
rest_command:
  lapsecam_quick_start:
    url: "http://<LapseCam IP>:19090/api/quick/start"
    method: POST
  lapsecam_quick_stop:
    url: "http://<LapseCam IP>:19090/api/quick/stop"
    method: POST
```

### 2. 示例：拓竹打印机自动录制

（实体与状态名以你 HA 里实际的打印机设备为准）

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

打印结束时 LapseCam 自动把本次打印的所有帧合成 MP4，随时在 Web 后台回放或下载。暂停即出片；恢复打印时再触发一次 start 会另起一段录制。

### 3. 示例：每天日出到日落自动延时

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

> 快捷录制参数（间隔/FPS/分辨率/任务名）由配置 `quick` 段控制，默认 5 秒/30FPS/1280×720。完整接口与配置说明见 [API 与配置参考](docs/api.md)。

## 🖥️ Web 管理后台

浏览器打开 `http://<IP>:<端口>` 即可使用三个面板：

- **摄像头**：增删改查、测试连接、在线/离线状态、启用开关
- **延时任务**：新建任务（摄像头/间隔/FPS/分辨率/开始与结束时间）、手动开始/停止、实时进度与帧数
- **视频**：播放/下载 MP4、删除记录

## 🔄 工作原理

```
RTSP ─▶ ffmpeg 抽帧（fps=1/间隔）─▶ data/frames/task-{id}/%06d.jpg
                                              │
                              任务结束/手动停止 │
                                              ▼
        ffmpeg 编码（-framerate fps -c:v libx264）─▶ data/videos/task-{id}/*.mp4
```

- 抽帧与成片解耦：RTSP 断线不影响已拍照片，重连后帧号自动接续
- 断线自动重连，退避序列 `5s → 10s → 30s → 60s`（可配置），连接稳定后重置
- 容器/服务重启自动恢复：`running` 任务继续抽帧，`stopping` 任务完成编码收尾
- 任务状态机：`pending → running → stopping → completed / failed / stopped`

## 📁 数据目录

```
data/
├── database.db                  # SQLite
├── frames/task-{id}/%06d.jpg    # 抽帧图片
├── videos/task-{id}/*.mp4       # 成片
└── logs/task-{id}.log           # ffmpeg 日志
```

## 📄 文档

- [API 与配置参考](docs/api.md)：全部接口、参数、响应与配置字段

## 🗺️ 路线图（暂未实现）

- ONVIF 自动发现 / 自动获取 RTSP
- Web 实时预览 / WebSocket
- 时间水印 / 天气信息
- 硬件编码 / GPU
- 快捷录制指定摄像头 / 多路并发
