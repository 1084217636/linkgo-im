# 编译阶段
FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY . .
RUN go mod download
# 编译对应的二进制文件 (注意修改路径)
RUN go build -o main ./cmd/gateway/main.go 

# 运行阶段
FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/main .
EXPOSE 8090
CMD ["./main"]