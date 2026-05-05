# AI Agent Workflow Demo

这个目录不是完整 AI 产品，而是为了展示“AI 接入研发流程”时需要的工程闭环：

- 配置检查：模拟游戏策划配置或服务端配置交付前校验。
- 测试建议：扫描 Go 代码，找出缺少测试覆盖的函数并生成补测建议。
- 质量 summary：记录验证命令、变更文件、失败原因、是否需要人工接管。

这些工具可以作为 Agent Workflow 里的工具节点使用：

```bash
make ai-config-check
make ai-test-suggest
make ai-quality-summary
make ai-demo
```

如果本机没有安装 `make`，可以直接运行：

```bash
bash scripts/ai_demo.sh
```

输出文件默认写入 `artifacts/`，该目录已加入 `.gitignore`。

## 对应简历表达

可以写成：

> 支持对配置类文件进行字段完整性、引用关系、重复 ID 与数值范围检查，可用于策划配置/服务端配置的交付前校验。

> 支持根据函数签名和历史测试文件生成测试建议，辅助补全基础单元测试与边界用例。

> 设计任务执行质量评估记录，统计修改文件数、验证结果、失败原因与人工接管标记，用于复盘 AI 工作质量。
