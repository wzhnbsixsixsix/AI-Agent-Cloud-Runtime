# Dashboard 前端测试：一页启动

## 1. 前置要求

- Docker Desktop 已启动。
- Docker Compose v2 已安装。
- 已准备智谱 API Key。

## 2. 首次准备

```bash
cd "/Users/Thomas/Desktop/AI Cloud Runtime Project/AI-Agent-Cloud-Runtime"
test -f .env || cp .env.example .env
```

在 `.env` 中确认：

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

Apple Silicon 首次或 Docker Desktop 清理数据后执行一次：

```bash
docker context use desktop-linux
docker buildx use desktop-linux
```

## 3. 完整启动

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

## 4. 只重建 Web

```bash
DOCKER_DEFAULT_PLATFORM=linux/arm64 docker compose \
  --env-file .env \
  -f deploy/docker-compose.yml \
  up -d --build --no-deps web
```

## 5. 状态检查

```bash
docker compose --env-file .env -f deploy/docker-compose.yml ps
```

需要 running：

```text
agentforge-web
agentforge-controlplane
agentforge-gateway
agentforge-worker-1
```

需要 healthy：

```text
agentforge-redis
agentforge-postgres
```

## 6. 前端验收

打开 `http://localhost:5173`：

```text
Agents → Create Agent → 填写配置 → Create
Agent Detail → Run task → 输入任务 → Run
Agent Detail → Workspace → 查看只读文件
Agent Detail → Stop → Start → Delete
Runs → 查看全局运行历史
使用指南 → 查看 Dashboard 能力和边界
```

## 7. 日志与停止

```bash
docker compose --env-file .env -f deploy/docker-compose.yml \
  logs -f web controlplane gateway worker
```

```bash
docker compose --env-file .env -f deploy/docker-compose.yml down
```

详细启动与故障排查见 [`STARTUP_GUIDE.md`](../STARTUP_GUIDE.md)，容器通信见 [`CONTAINER_NETWORKING.md`](./CONTAINER_NETWORKING.md)。
