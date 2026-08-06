# LinkGo 文档入口

当前文档只保留一条学习主线。不要从代码地图、旧面经、知识库或历史计划随机开始。

## 第一次学习项目

从 [LinkGo 从零学习手册](handbook/README.md) 开始，严格按 `00_START_HERE.md` 到 `21_CHECKLIST.md` 的编号阅读。`docs/handbook/` 是唯一学习目录；旧的 `docs/learning/` 摘要已合并后删除。

这套手册假设你只会 Go 基本语法。每章只使用前面已经解释过的知识，并包含前置、新概念、项目链路、选择理由、精确到函数/字段的代码阅读任务、练习和闭卷题。参考答案就在同一章题目后面；请先独立作答，再逐题核对，不需要跳到另一份文档找答案。

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

- [按职责查包和入口，不按它学习概念](reference/CODE_MAP.md)：文件和包索引，只在已经知道要找哪类入口时使用。
- [核对登录、单聊、群聊的函数级调用顺序](reference/CORE_LINKS.md)：学完手册对应链路后查漏，不替代逐章阅读。
- [核对模块的真实文件、符号和边界](reference/MODULE_CARDS.md)：只在源码定位或面试前抽查时使用。

### runbooks

- [按步骤启动本地演示并核对预期结果](runbooks/LOCAL_DEMO.md)：学完相关章节后执行，不用它学习概念。
- [执行 Docker、CI、Kubernetes 命令并判断输出](runbooks/DEVOPS.md)：学完第 17、18 章后作为操作手册。

### evidence

- [核对当前验证命令、结果与它们不能证明的内容](evidence/CURRENT_VERIFICATION.md)：发布或写简历前检查证据边界。

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
