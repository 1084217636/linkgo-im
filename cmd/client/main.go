package main

import (
	"fmt"
	"net"
	"time"

	"github.com/1084217636/linkgo-im/api"
	"github.com/1084217636/linkgo-im/pkg/codec"
	"google.golang.org/protobuf/proto"
)

func main() {
	// 1. 连接网关
	conn, err := net.Dial("tcp", "127.0.0.1:9000")
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	// --- 步骤 A: 登录 (握手) ---
	// 按照我们现在的网关逻辑，第一个包的内容就是 UserId
	user := "UserA"
	loginPkg, _ := codec.Encode([]byte(user))
	conn.Write(loginPkg)
	fmt.Printf("✅ [%s] 登录包已发送\n", user)

	// 给一点时间让网关处理登录
	time.Sleep(time.Second)

	// --- 步骤 B: 发送消息给 UserB ---
	//三步骤1.填表写信息，2.压缩数据,3.装箱封装数据包 
	//协议栈封装的标准写法，所有请求都是按照这三部分

	// 构造 Protobuf 消息
	msg := &api.ChatMessage{
		FromUserId: user,
		ToUserId:   "UserB", // 关键：我要发给 B
		Content:    "你好 UserB! 我是 UserA，你收到我的消息了吗？",
		Seq:        1001,
	}

	// 序列化 + 封包
	pbData, _ := proto.Marshal(msg)
	pkg, _ := codec.Encode(pbData)




	// 发送
	conn.Write(pkg)//conn.Write(pkg)就是发送pkg
	fmt.Printf("🚀 [%s] 消息已发送给 UserB\n", user)

	// --- 步骤 C: 阻塞等待 (防止程序退出) ---
	// 同时也监听看看有没有回信
	readLoop(conn, user)
}

func readLoop(conn net.Conn, user string) {
	for {
		// 使用我们封装的 Decode 读取完整包
		data, err := codec.Decode(conn)
		if err != nil {
			fmt.Println("连接断开:", err)
			return
		}

		// 尝试解析打印
		msg := &api.ChatMessage{}
		proto.Unmarshal(data, msg)
		fmt.Printf("📩 [%s] 收到: %s 说: %s\n", user, msg.FromUserId, msg.Content)
	}
}