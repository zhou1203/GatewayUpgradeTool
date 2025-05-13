# 第一阶段：构建 Go 应用
FROM golang:1.24 AS builder

# 设置工作目录
WORKDIR /app

# 拷贝 go.mod 和 go.sum 文件
COPY go.mod go.sum ./

# 预下载依赖
RUN go mod download

# 拷贝所有源码
COPY . .

# 编译可执行文件
RUN CGO_ENABLED=0 GOOS=linux go build -o gateway-upgrade-tool ./cmd/gatewayupgradetool

# 第二阶段：创建最小镜像
FROM alpine:3.19

# 设置工作目录
WORKDIR /app

# 拷贝编译好的二进制文件
COPY --from=builder /app/gateway-upgrade-tool .

# 设置默认入口，可传参数，如 --gateways
ENTRYPOINT ["./gateway-upgrade-tool"]