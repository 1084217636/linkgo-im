这个是存放协议的地方，规定客户端和服务端怎么聊天的
# 在项目根目录下运行
protoc --go_out=. --go-grpc_out=. api/protocol.proto