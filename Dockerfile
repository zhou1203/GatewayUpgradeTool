# syntax=docker/dockerfile:1.4
# 第一阶段：构建 Go 应用
FROM golang:1.24 AS builder

# 设置工作目录
WORKDIR /app

# 拷贝 go.mod 和 go.sum 文件
COPY go.mod go.sum ./

# 预下载依赖
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=$TARGETARCH go build -o gateway-upgrade-tool ./cmd

FROM alpine:3.19

# 设置工作目录
WORKDIR /app

COPY --from=builder /app/gateway-upgrade-tool .

ENTRYPOINT ["./gateway-upgrade-tool"]