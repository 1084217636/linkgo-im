#!/bin/bash

# 1. 检查是否输入了提交信息
if [ -z "$1" ]
then
    echo "❌ 错误: 请输入提交信息 (commit message)"
    echo "用法: ./upload.sh '修复了跨域和Redis连接问题'"
    exit 1
fi

# 2. 初始化 Git (如果还没初始化的话)
if [ ! -d ".git" ]; then
    echo "🚀 初始化 Git 仓库..."
    git init
    # 这里替换成你自己的 GitHub 仓库地址
    git remote add origin https://github.com/1084217636/linkgo-im.git
fi

# 3. 添加所有文件
echo "📦 正在暂存文件..."
git add .

# 4. 提交
echo "💾 正在提交: $1"
git commit -m "$1"

# 5. 推送到 GitHub
# 注意：第一次推送建议手动执行一次 git push -u origin main
echo "⬆️ 正在推送到远程仓库..."
git push

if [ $? -eq 0 ]; then
    echo "✅ 上传成功！"
else
    echo "❌ 上传失败，请检查远程仓库配置或网络。"
fi
