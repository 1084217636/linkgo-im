# Project Structure

这份约定用于保持 LinkGo IM 项目结构稳定，后续升级时优先按目录职责放文件，避免把临时产物和工程文件混在一起。

## 根目录

- `README.md`、`Makefile`、`Dockerfile`、`docker-compose*.yml`：项目入口、构建和本地编排。
- `.env.docker-cn`：国内镜像加速的 Docker Compose 环境变量，可提交，里面不放真实密码。
- `.github/workflows/`：CI/CD 工作流。
- 根目录不放临时报告、截图、一次性脚本和大段学习文档。

## 代码目录

- `api/`：HTTP API、protobuf、gRPC 契约和生成代码。
- `cmd/`：Gateway、Logic、Transfer 等可执行进程入口，只做启动和依赖组装。
- `internal/`：业务实现、消息投递、健康检查、指标、服务内部组件。
- `pkg/`：可复用的基础库。

## 工程化目录

- `deploy/k8s/`：Kubernetes 清单和 kustomize 配置。
- `deploy/observability/`：Prometheus、Grafana 和面板配置。
- `benchmark/`：压测工具、压测脚本和压测报告。
- `scripts/`：可重复执行的辅助脚本。
- `sql/`：初始化 SQL 和迁移 SQL。

## 文档和产物

- `docs/`：简历、面试、架构、学习和工程化说明。
- `public/`：本地前端调试台，不承载正式前端工程。
- `artifacts/`：本地生成的验证报告，默认忽略，不提交。
- `升级文档/`：升级路线 PDF 和原始资料，保留原名，避免在业务目录里混放。

## 清理原则

- 新增能力优先放到已有职责目录，不新建含义模糊的目录。
- 临时文件进入 `artifacts/` 或 `/tmp`，不要放根目录。
- Windows `Zone.Identifier`、本地 IDE/AI 助手状态文件不提交。
- 已跟踪的历史材料先不直接删除；需要整理时单独做归档迁移并同步更新引用。
