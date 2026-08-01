# AgentForge Dashboard 启动指南

这份文档只负责通过 Docker Compose 启动 AgentForge，并从 Web Dashboard 验证当前主流程。CLI、RAG、Hook、Pipeline 和压测命令统一放在 [`docs/CLI_RUNTIME_GUIDE.md`](./docs/CLI_RUNTIME_GUIDE.md)。

## 1. 前置要求

需要安装：

- Docker Desktop
- Docker Compose v2
- macOS Terminal、iTerm、Windows WSL 或 Linux shell

确认 Docker Engine 正常：

```bash
docker version
docker compose version
```

## 2. 准备环境

进入项目根目录：

```bash
cd "/Users/Thomas/Desktop/AI Cloud Runtime Project/AI-Agent-Cloud-Runtime"
```

首次创建本地配置：

```bash
test -f .env || cp .env.example .env
```

在 `.env` 中填写智谱 API Key。`.env` 只保留在本地，不要提交：

```dotenv
LLM_PROVIDER=openai
OPENAI_BASE_URL=https://open.bigmodel.cn/api/paas/v4
OPENAI_API_KEY=你的智谱_API_KEY
OPENAI_MODEL=glm-4.7-flash
OPENAI_MAX_TOKENS=65536
LLM_THINKING_ENABLED=true
OPENAI_TIMEOUT_SECONDS=60s
WORKER_HEARTBEAT_SECONDS=5s

MODELSCOPE_BASE_URL=https://api-inference.modelscope.cn/v1
MODELSCOPE_ACCESS_TOKEN=你的_ModelScope_Access_Token
MODELSCOPE_MODEL=Qwen/Qwen3.5-35B-A3B
MODELSCOPE_MAX_TOKENS=0
MODELSCOPE_TIMEOUT_SECONDS=60s
```

两个密钥相互独立：

- `OPENAI_API_KEY`：智谱开放平台 Key，用于 `glm-4.7-flash`。
- `MODELSCOPE_ACCESS_TOKEN`：ModelScope Access Token，用于 `Qwen/Qwen3.5-35B-A3B`。

Dashboard 创建 Agent 时可选择模型。没有填写 ModelScope token 时，GLM 仍可正常使用，但 Qwen Run 会返回缺少 token 的明确错误。

## 3. Apple Silicon 首次设置

M1/M2/M3/M4 Mac 或 Docker Desktop 清理数据后执行一次：

```bash
docker context use desktop-linux
docker buildx use desktop-linux
docker context show
```

最后一条应输出 `desktop-linux`。

Intel Mac 或 Linux 不需要强制 `linux/arm64`，直接使用本机默认平台。

## 4. 构建并启动

Apple Silicon：

```bash
DOCKER_DEFAULT_PLATFORM=linux/arm64 docker compose \
  --env-file .env \
  -f deploy/docker-compose.yml \
  up -d --build
```

其他平台：

```bash
docker compose --env-file .env -f deploy/docker-compose.yml up -d --build
```

首次启动需要拉取基础镜像并构建八个 AgentForge 镜像，耗时取决于 Docker Hub 网络和本机性能。

检查状态：

```bash
docker compose --env-file .env -f deploy/docker-compose.yml ps
```

至少应满足：

- `agentforge-web`、`agentforge-controlplane`、`agentforge-gateway`、`agentforge-worker-1` 为 running。
- `agentforge-redis`、`agentforge-postgres` 为 healthy。

各服务的职责和内部地址见 [`docs/CONTAINER_NETWORKING.md`](./docs/CONTAINER_NETWORKING.md)。

## 5. Dashboard 验收

打开：

```text
http://localhost:5173
```

按以下顺序验证：

1. 在 **Agents** 页点击 **Create Agent**。
2. 填写名称、角色、系统提示词；高级配置可设置资源额度、工具 allow-list 和 workspace 保留策略。
3. 创建后等待状态变为 `running`。
4. 进入详情页，在 **Run task** 提交任务并观察状态及流式输出。
5. 查看 **Recent runs** 和右侧只读 **Workspace**。
6. 使用 **Stop**、**Start**、**Delete** 验证生命周期操作。
7. 在 **Runs** 查看全局运行记录，在 **使用指南** 查看 Dashboard 能力边界。

验收成功标准：

- Dashboard 正常显示，不是空白页。
- Agent 可以创建并获得独立容器与 workspace volume。
- 所选模型的输出可以实时显示。
- 同一 Agent 同时只允许一个活跃 run。
- Agent 可以停止、恢复和删除。

当前 UI 不提供 ACP 多 Agent 协作拓扑、RAG 文档管理、workspace 在线编辑、登录、多租户或 RBAC。

## 6. 常用维护命令

查看服务：

```bash
docker compose --env-file .env -f deploy/docker-compose.yml ps
```

查看主链路日志：

```bash
docker compose --env-file .env -f deploy/docker-compose.yml \
  logs -f web controlplane gateway worker
```

只重建 Web：

```bash
DOCKER_DEFAULT_PLATFORM=linux/arm64 docker compose \
  --env-file .env \
  -f deploy/docker-compose.yml \
  up -d --build --no-deps web
```

停止并保留数据：

```bash
docker compose --env-file .env -f deploy/docker-compose.yml down
```

停止并删除 Compose 数据卷：

```bash
docker compose --env-file .env -f deploy/docker-compose.yml down -v
```

最后一条会删除 Compose 管理的本地数据，只在明确不需要测试数据时使用。

## 7. 常见故障

| 现象 | 处理方式 |
|---|---|
| Docker Hub OAuth timeout | 检查热点/VPN/TUN 网络；先单独 `docker pull` 失败的基础镜像，再重试 Compose。 |
| `exec format error` | 用 `docker version --format 'Docker Engine: {{.Server.Arch}}'` 确认架构，并使用匹配的平台。 |
| BuildKit `metadata_v2.db` I/O error | 重启 Docker Desktop；仍失败时使用 Troubleshoot 清理 Docker 数据，注意先备份重要 volume。 |
| Dashboard 空白 | 强制刷新并查看 `web` 日志。 |
| 创建 Agent 失败 | 查看 `controlplane` 日志，确认 Docker socket 可用且 `alpine:3.19` 能拉取。 |
| Run 无输出 | GLM 检查 `OPENAI_API_KEY`；Qwen 检查 `MODELSCOPE_ACCESS_TOKEN`；再查看 `worker`、`gateway` 日志。 |
| API 请求失败 | 确认 `web`、`controlplane` 都在运行；Nginx 会把 `/api` 代理到 `controlplane:8086`。 |

## 8. 继续阅读

- 一页命令清单：[`docs/FRONTEND_TEST_QUICKSTART.md`](./docs/FRONTEND_TEST_QUICKSTART.md)
- 容器通信：[`docs/CONTAINER_NETWORKING.md`](./docs/CONTAINER_NETWORKING.md)
- CLI 运行时手册：[`docs/CLI_RUNTIME_GUIDE.md`](./docs/CLI_RUNTIME_GUIDE.md)
- 当前架构：[`docs/ARCHITECTURE.md`](./docs/ARCHITECTURE.md)
- 文档索引：[`docs/README.md`](./docs/README.md)
