# ---- build stage ----
FROM golang:1.25-alpine AS builder

WORKDIR /src

# 先拷贝依赖文件，充分利用构建缓存
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/app ./cmd/server

# ---- runtime stage ----
FROM alpine:3.20

# FFmpeg 负责 RTSP 拉流 / 抽帧 / H.264 编码
RUN apk add --no-cache ffmpeg ca-certificates tzdata wget

# go2rtc 负责 Web 实时预览（RTSP → 浏览器可播的 MSE/HLS）
# 版本与二进制命名见 https://github.com/AlexxIT/go2rtc/releases
ARG GO2RTC_VERSION=v1.9.14
ARG TARGETARCH
RUN case "$TARGETARCH" in \
      amd64) G2R_ARCH=amd64 ;; \
      arm64) G2R_ARCH=arm64 ;; \
      arm)   G2R_ARCH=arm ;; \
      *) echo "unsupported TARGETARCH: $TARGETARCH" >&2; exit 1 ;; \
    esac && \
    wget -qO /usr/local/bin/go2rtc "https://github.com/AlexxIT/go2rtc/releases/download/${GO2RTC_VERSION}/go2rtc_linux_${G2R_ARCH}" && \
    chmod +x /usr/local/bin/go2rtc && \
    go2rtc -version

WORKDIR /app

COPY --from=builder /out/app /app/app
COPY config/config.yaml /app/config/config.yaml

# 数据目录（通过 docker-compose 挂载持久化）
RUN mkdir -p /app/data/frames /app/data/videos /app/logs

EXPOSE 8080

VOLUME ["/app/data", "/app/config"]

HEALTHCHECK --interval=30s --timeout=3s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8080/api/health >/dev/null 2>&1 || exit 1

ENTRYPOINT ["/app/app", "-config", "/app/config/config.yaml"]
