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
RUN apk add --no-cache ffmpeg ca-certificates tzdata

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
