# LinkGo 文档入口

当前文档只保留一条学习主线。不要从代码地图、旧面经或版本计划随机开始。

## 第一次学习项目

从 [handbook/README.md](handbook/README.md) 开始，按 `00 → 21` 顺序阅读。

这套手册假设你只会 Go 基本语法。每章只使用前面已经解释过的知识，并固定包含前置知识、新概念、项目链路、代码锚点、练习和闭卷题。

## 其他目录

```text
docs/
├── handbook/   唯一学习主线
├── reference/  学完对应章节后查阅的代码参考
├── runbooks/   运行、演示和部署操作
├── evidence/   测试与验证记录
└── knowledge/  AI 问答运行时读取的稳定知识语料
```

### reference

- [CODE_MAP.md](reference/CODE_MAP.md)：文件和包索引，不是教材。
- [CORE_LINKS.md](reference/CORE_LINKS.md)：函数级调用链参考。
- [MODULE_CARDS.md](reference/MODULE_CARDS.md)：当前模块速查卡，仅在源码定位时使用。

### runbooks

- [LOCAL_DEMO.md](runbooks/LOCAL_DEMO.md)：本地演示命令和验收步骤。
- [DEVOPS.md](runbooks/DEVOPS.md)：Docker、CI、Kubernetes 操作参考。

### evidence

- [CURRENT_VERIFICATION.md](evidence/CURRENT_VERIFICATION.md)：当前验证命令、结果和证据边界。

### knowledge

这个目录由 AI FAQ/RAG 功能读取。它是程序数据来源，不是你的学习顺序，也不放简历话术和历史计划。

## 已移除的资料

旧版“最终学习包、秋招面经、学习地图、版本计划、背诵稿”内容重复且部分与当前代码冲突，已经从当前分支移除。需要追溯项目演化时使用 Git 历史，不要把旧稿当作当前事实。

## 事实优先级

出现冲突时按以下顺序判断：

```text
当前源码与 SQL
→ 当前自动化测试
→ docs/handbook
→ reference/evidence 中的辅助材料
```
