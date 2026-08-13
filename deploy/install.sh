#!/usr/bin/env bash
# LapseCam 在 Armbian 上一键安装为 systemd 服务
#
# 用法（从任意目录运行，默认从 /root/lapsecam 读取程序文件）：
#   sudo bash install.sh                                # 自动检测 arm64 / arm
#   sudo bash install.sh arm64                          # 强制 64 位
#   LAPSECAM_SRC=/path/to/program sudo bash install.sh  # 指定程序目录
#
# 程序目录里需要有以下文件（自动识别）：
#   平铺：   lapsecam-linux-<arch> 或 lapsecam、config.arm.yaml 或 config.yaml、lapsecam.service
#   仓库：   dist/lapsecam-linux-<arch>、config/config.arm.yaml、deploy/lapsecam.service
set -euo pipefail

if [ "$(id -u)" -ne 0 ]; then
  echo "请用 root 运行：sudo bash install.sh" >&2
  exit 1
fi

SRC="${LAPSECAM_SRC:-/root/lapsecam}"
if [ ! -d "$SRC" ]; then
  echo "程序目录不存在：$SRC（可用 LAPSECAM_SRC=/path 指定）" >&2
  exit 1
fi
echo "==> 程序目录：$SRC"

# ---- 1. 架构检测 ----
REQ_ARCH="${1:-auto}"
case "$REQ_ARCH" in
  auto)
    case "$(uname -m)" in
      aarch64|arm64) ARCH=arm64 ;;
      armv6l|armv7l|armhf|arm) ARCH=arm ;;
      *)
        echo "不支持的架构：$(uname -m)，请手动指定 arm64 或 arm" >&2
        exit 1
        ;;
    esac
    ;;
  arm64|arm) ARCH="$REQ_ARCH" ;;
  *)
    echo "参数只能是 arm64 或 arm" >&2
    exit 1
    ;;
esac
echo "==> 目标架构：$ARCH"

# ---- 2. 定位程序文件（平铺 / 仓库两种布局都支持） ----
BIN=""
# 32 位 ARM 的历史产物命名是 armv7，这里两种都兼容
for p in "$SRC/lapsecam-linux-$ARCH" "$SRC/lapsecam-linux-armv7" "$SRC/lapsecam" \
        "$SRC/dist/lapsecam-linux-$ARCH" "$SRC/dist/lapsecam-linux-armv7"; do
  if [ -f "$p" ]; then BIN="$p"; break; fi
done
[ -n "$BIN" ] || { echo "在 $SRC 下找不到二进制（lapsecam-linux-$ARCH / lapsecam / dist/...）" >&2; exit 1; }

CONF=""
for p in "$SRC/config.arm.yaml" "$SRC/config.yaml" "$SRC/config/config.arm.yaml"; do
  if [ -f "$p" ]; then CONF="$p"; break; fi
done
[ -n "$CONF" ] || { echo "在 $SRC 下找不到配置（config.arm.yaml / config.yaml / config/...）" >&2; exit 1; }

UNIT=""
for p in "$SRC/lapsecam.service" "$SRC/deploy/lapsecam.service"; do
  if [ -f "$p" ]; then UNIT="$p"; break; fi
done
[ -n "$UNIT" ] || { echo "在 $SRC 下找不到服务单元（lapsecam.service 或 deploy/lapsecam.service）" >&2; exit 1; }

echo "==> 二进制：$BIN"
echo "==> 配置：$CONF"
echo "==> 服务单元：$UNIT"

# ---- 3. 安装 ffmpeg（缺才装） ----
if ! command -v ffmpeg >/dev/null 2>&1 || ! command -v ffprobe >/dev/null 2>&1; then
  echo "==> 未检测到 ffmpeg，正在通过 apt 安装 ..."
  export DEBIAN_FRONTEND=noninteractive
  apt-get update
  apt-get install -y ffmpeg
fi
ffmpeg -version >/dev/null 2>&1 || { echo "ffmpeg 安装失败，请手动安装后重试" >&2; exit 1; }
echo "==> ffmpeg OK：$(ffmpeg -version 2>/dev/null | head -1)"

# ---- 4. 创建目录 ----
# 数据目录与 config.arm.yaml 里的 storage.baseDir /mnt/data/lapsecam 保持一致
install -d /opt/lapsecam /etc/lapsecam /mnt/data/lapsecam

# ---- 5. 安装二进制 / 配置 / 服务单元 ----
install -m 0755 "$BIN" /usr/local/bin/lapsecam
install -m 0644 "$CONF" /etc/lapsecam/config.yaml
install -m 0644 "$UNIT" /etc/systemd/system/lapsecam.service
echo "==> 已安装：/usr/local/bin/lapsecam"
echo "==> 已安装：/etc/lapsecam/config.yaml（如需改端口/存储路径，改这里）"

# ---- 6. 注册并启动服务 ----
systemctl daemon-reload
systemctl enable --now lapsecam
systemctl restart lapsecam
echo "==> 服务已注册并启动"

# ---- 7. 验证 ----
sleep 2
if systemctl is-active --quiet lapsecam; then
  echo "==> 服务状态："
  systemctl status lapsecam --no-pager | head -8
  PORT="$(sed -n 's/^[[:space:]]*addr:[[:space:]]*//p' /etc/lapsecam/config.yaml | tr -d ':"')"
  echo "==> 健康检查：http://127.0.0.1:$PORT/api/health"
  if command -v curl >/dev/null 2>&1; then
    curl -fsS "http://127.0.0.1:$PORT/api/health" || echo "（进程在运行，但健康检查未通过，请查看 journalctl -u lapsecam）"
  elif command -v wget >/dev/null 2>&1; then
    wget -qO- "http://127.0.0.1:$PORT/api/health" || echo "（进程在运行，但健康检查未通过，请查看 journalctl -u lapsecam）"
  fi
  echo
  echo "==> 安装完成。Web 后台：http://<本机IP>:$PORT"
  echo "    查看日志：journalctl -u lapsecam -f"
else
  echo "==> 服务启动失败，最近日志：" >&2
  journalctl -u lapsecam -n 30 --no-pager >&2 || true
  exit 1
fi
