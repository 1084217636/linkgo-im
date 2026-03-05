FROM golang:1.25.7-alpine AS builder
WORKDIR /app
# 设置代理，解决国内下载慢
ENV GOPROXY=https://goproxy.cn,direct
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o gateway ./cmd/gateway/main.go
RUN go build -o logic ./cmd/logic/main.go

FROM alpine:latest
WORKDIR /root/
# 安装基础库
RUN apk add --no-cache ca-certificates libc6-compat
COPY --from=builder /app/gateway .
COPY --from=builder /app/logic .
# 给执行权限
RUN chmod +x /root/gateway /root/logic