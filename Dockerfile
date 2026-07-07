ARG GO_BUILDER_IMAGE=golang:1.25-alpine
ARG RUNTIME_IMAGE=alpine:3.22
FROM ${GO_BUILDER_IMAGE} AS builder
WORKDIR /app
# 设置代理，解决国内下载慢
ENV GOPROXY=https://goproxy.cn,direct
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o gateway ./cmd/gateway
RUN go build -o logic ./cmd/logic
RUN go build -o transfer ./cmd/transfer

FROM ${RUNTIME_IMAGE}
WORKDIR /root/
# 安装基础库
RUN apk add --no-cache ca-certificates libc6-compat
COPY --from=builder /app/gateway .
COPY --from=builder /app/logic .
COPY --from=builder /app/transfer .
COPY --from=builder /app/cmd/gateway/etc /root/cmd/gateway/etc
COPY --from=builder /app/cmd/logic/etc /root/cmd/logic/etc
COPY --from=builder /app/README.md /root/README.md
COPY --from=builder /app/docs /root/docs
# 给执行权限
RUN chmod +x /root/gateway /root/logic /root/transfer
