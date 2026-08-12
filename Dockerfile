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
RUN apk add --no-cache ca-certificates libc6-compat \
    && addgroup -S -g 10001 linkgo \
    && adduser -S -D -H -u 10001 -G linkgo linkgo
WORKDIR /app
COPY --from=builder --chown=10001:10001 /app/gateway .
COPY --from=builder --chown=10001:10001 /app/logic .
COPY --from=builder --chown=10001:10001 /app/transfer .
COPY --from=builder --chown=10001:10001 /app/cmd/gateway/etc /app/cmd/gateway/etc
COPY --from=builder --chown=10001:10001 /app/cmd/logic/etc /app/cmd/logic/etc
COPY --from=builder --chown=10001:10001 /app/README.md /app/README.md
COPY --from=builder --chown=10001:10001 /app/docs /app/docs
RUN chmod 0555 /app/gateway /app/logic /app/transfer
USER 10001:10001
