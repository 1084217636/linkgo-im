package main

import (
	"fmt"
	"net"
	"time"

	"github.com/1084217636/linkgo-im/api"
	"github.com/1084217636/linkgo-im/pkg/codec"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"google.golang.org/protobuf/proto"
)

// --- 全局变量 ---
var (
	app          *tview.Application
	chatView     *tview.TextView   // 聊天记录显示区
	inputField   *tview.InputField // 输入框
	userList     *tview.List       // 左侧好友列表
	conn         net.Conn
	myUserId     string
	targetUserId string = "UserB" // 默认发给 UserB
)

func main() {
	app = tview.NewApplication()

	// 直接启动登录弹窗
	showLoginModal()

	if err := app.Run(); err != nil {
		panic(err)
	}
}

func showLoginModal() {
	modal := tview.NewModal().
		SetText("请选择你的身份").
		AddButtons([]string{"UserA", "UserB", "UserC", "退出"}).
		SetDoneFunc(func(buttonIndex int, buttonLabel string) {
			if buttonLabel == "退出" {
				app.Stop()
			} else {
				myUserId = buttonLabel
				// 简单的自动匹配逻辑：你是A就发给B，你是B就发给A
				if myUserId == "UserA" {
					targetUserId = "UserB"
				}
				if myUserId == "UserB" {
					targetUserId = "UserA"
				}

				startChatUI() // 进入主界面
			}
		})

	app.SetRoot(modal, false)
}

func startChatUI() {
	// 1. 连接服务器
	var err error
	conn, err = net.Dial("tcp", "127.0.0.1:9000")
	if err != nil {
		panic("连接服务器失败: " + err.Error())
	}

	// 发送登录包
	loginPkg, _ := codec.Encode([]byte(myUserId))
	conn.Write(loginPkg)

	// 2. 构建 UI 布局

	// A. 左侧好友列表
	userList = tview.NewList().ShowSecondaryText(false)
	userList.SetBorder(true).SetTitle(" 好友列表 ")
	// 硬编码几个好友
	users := []string{"UserA", "UserB", "UserC", "UserD"}
	for _, u := range users {
		if u == myUserId {
			continue
		} // 别显示自己
		userList.AddItem(u, "", 0, func() {
			// 点击事件
			idx := userList.GetCurrentItem()
			mainText, _ := userList.GetItemText(idx)
			targetUserId = mainText
			inputField.SetLabel("发给 " + targetUserId + ": ")
		})
	}

	// B. 右侧聊天记录
	chatView = tview.NewTextView().
		SetDynamicColors(true).
		SetRegions(true).
		SetWordWrap(true).
		SetChangedFunc(func() {
			app.Draw()
		})
	chatView.SetBorder(true).SetTitle(fmt.Sprintf(" 消息记录 (%s) ", myUserId))

	// C. 底部输入框
	inputField = tview.NewInputField().
		SetLabel("发给 " + targetUserId + ": ").
		SetFieldWidth(0).
		SetAcceptanceFunc(nil)

	// 回车发送逻辑
	inputField.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEnter {
			text := inputField.GetText()
			if text == "" {
				return
			}

			sendMessage(text)

			// 清空输入框
			inputField.SetText("")
		}
	})

	// D. 布局组装 (Flex Layout)
	// 上半部分：左边列表 + 右边聊天
	topFlex := tview.NewFlex().
		AddItem(userList, 0, 1, false). // 左侧占 1 份
		AddItem(chatView, 0, 4, false)  // 右侧占 4 份

	// 整体：上半部分 + 底部输入框
	mainFlex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(topFlex, 0, 1, false).
		AddItem(inputField, 3, 1, true) // 底部占 3 行，默认获取焦点

	app.SetRoot(mainFlex, true)

	// 3. 启动后台协程接收消息
	go receiveLoop()
}

func sendMessage(content string) {
	// 1. 本地显示
	timestamp := time.Now().Format("15:04:05")
	fmt.Fprintf(chatView, "[yellow]%s [我] -> [%s]: %s[white]\n", timestamp, targetUserId, content)

	// 2. 发送给服务器
	msg := &api.ChatMessage{
		FromUserId: myUserId,
		ToUserId:   targetUserId,
		Content:    content,
	}
	pbData, _ := proto.Marshal(msg)
	pkg, _ := codec.Encode(pbData)
	conn.Write(pkg)
}

func receiveLoop() {
	for {
		data, err := codec.Decode(conn)
		if err != nil {
			// 必须在 UI 线程更新
			app.QueueUpdateDraw(func() {
				fmt.Fprintf(chatView, "[red]连接断开: %v[white]\n", err)
			})
			return
		}

		msg := &api.ChatMessage{}
		proto.Unmarshal(data, msg)

		// UI 更新必须放入 QueueUpdateDraw
		app.QueueUpdateDraw(func() {
			timestamp := time.Now().Format("15:04:05")
			fmt.Fprintf(chatView, "[green]%s [%s]: %s[white]\n", timestamp, msg.FromUserId, msg.Content)
		})
	}
}