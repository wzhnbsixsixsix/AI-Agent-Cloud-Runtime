# AgentForge 启动文档

这份文档写给：**懂系统设计，但不熟 Go 的人**。

你不需要先理解 Go 代码，也不需要本机安装 Go。推荐路线是用 Docker Compose 把整套系统拉起来，再用一个 Docker 版 `agentctl` 客户端去调用 gateway。

---

## 1. 你将启动什么

AgentForge 当前是一套云原生多智能体运行时。启动后会有这些角色：

| 组件 | 系统设计里的角色 | 默认端口 | 说明 |
|---|---|---:|---|
| `gateway` | API Gateway / 接入层 | `8080` gRPC, `8090` ACP | 接收 CLI 请求，投递 Run 任务，回传流式事件 |
| `scheduler` | Worker 注册中心 | `8081`, `8082` | W1-W6 主要做 worker 注册和心跳 |
| `worker` | 执行节点 | 无外部端口 | 消费任务，调用 LLM，执行 tool，注入 Skill/RAG 上下文 |
| `redis` | 队列 + Pub/Sub + History | `6379` | Redis Stream 任务队列、事件广播、历史存储 |
| `postgres` | RAG 存储 | `5432` | pgvector 存储文档 chunk 和 embedding |
| `agentctl` | 客户端 CLI | 无服务端口 | 发起 run/tool/rag 命令 |

核心链路：

```text
agentctl
  -> gateway
  -> Redis Stream queue:agent_tasks
  -> worker
  -> LLM / Tool / Skill / RAG
  -> Redis PubSub events:{run_id}
  -> gateway
  -> agentctl 流式输出
```

---

## 2. 前置要求

必须安装：

- Docker Desktop
- Docker Compose v2
- 一个终端：macOS Terminal / iTerm / Windows WSL / Linux shell

可选安装：

- GNU Make：有它可以用 `make up` / `make down`
- Go 1.22+：只有你想本地编译二进制时才需要

确认 Docker 可用：

```bash
docker version
docker compose version
```

如果这两个命令失败，先启动 Docker Desktop。

---

## 3. 第一次启动：最稳妥的 Mock 模式

Mock 模式不会调用真实大模型 API，适合先验证系统链路。

### 3.1 准备配置

在项目根目录执行：

```bash
cp .env.example .env
```

打开 `.env`，先改成下面几项：

```dotenv
LLM_PROVIDER=mock
SANDBOX_DRIVER=memory
RAG_ENABLED=false
```

解释一下：

- `LLM_PROVIDER=mock`：worker 不访问 OpenAI，只返回固定模拟 token。
- `SANDBOX_DRIVER=memory`：先不用 Docker-in-Docker 风格的 sandbox，降低启动门槛。
- `RAG_ENABLED=false`：第一次先不开 RAG，等主链路跑通后再开。

### 3.2 启动服务

如果你有 Make：

```bash
make up
```

如果没有 Make，直接用 Docker Compose：

```bash
docker compose --env-file .env -f deploy/docker-compose.yml up -d --build
```

第一次会比较慢，因为要拉镜像并构建：

- `redis:7-alpine`
- `pgvector/pgvector:pg16`
- `bufbuild/buf`
- `golang:1.22-alpine`
- `gcr.io/distroless/static-debian12`

### 3.3 看服务是否起来

```bash
docker compose --env-file .env -f deploy/docker-compose.yml ps
```

你希望看到：

```text
agentforge-redis       running / healthy
agentforge-postgres    running / healthy
agentforge-gateway     running
agentforge-scheduler   running
agentforge-worker-*    running
```

看日志：

```bash
docker compose --env-file .env -f deploy/docker-compose.yml logs -f
```

看到类似这些日志就说明主链路在工作：

```text
gateway booting
grpc serving addr=:8080
scheduler registered
worker booting
skill selector loaded
```

按 `Ctrl-C` 只会退出日志查看，不会停止服务。

---

## 4. 准备客户端 agentctl

`make up` 会构建 `gateway/scheduler/worker`，但不会自动把 `agentctl` 放到你的本机 PATH。对不懂 Go 的用户，推荐构建一个 Docker 版 CLI。

### 4.1 构建 Docker 版 agentctl

在项目根目录执行：

```bash
docker build -f build/Dockerfile --build-arg BIN=agentctl -t agentforge-agentctl .
```

这条命令会用同一个 Dockerfile 构建 CLI，不需要你本机装 Go。

### 4.2 用 agentctl 跑一次 Run

macOS / Windows Docker Desktop 推荐这样连宿主机端口：

```bash
docker run --rm agentforge-agentctl \
  run \
  --addr host.docker.internal:8080 \
  --prompt "用一句话介绍 AgentForge"
```

Linux 如果没有 `host.docker.internal`，可以改成：

```bash
docker run --rm --network host agentforge-agentctl \
  run \
  --addr localhost:8080 \
  --prompt "用一句话介绍 AgentForge"
```

Mock 模式下你会看到类似：

```text
[mock recv: 用一句话介绍 AgentForge] Hello, I am AgentForge.
[DONE] run_id=... trace_id=... tokens=...
```

这说明：

```text
CLI -> gateway -> Redis -> worker -> mock LLM -> gateway -> CLI
```

整条链路已经跑通。

---

## 5. 切换到真实 LLM

当 Mock 模式正常后，再接真实 OpenAI 兼容 API。

编辑 `.env`：

```dotenv
LLM_PROVIDER=openai
OPENAI_BASE_URL=https://api.openai.com/v1
OPENAI_API_KEY=sk-your-key
OPENAI_MODEL=gpt-4o-mini
```

如果你用 DeepSeek、通义、Moonshot 等 OpenAI 兼容服务：

```dotenv
OPENAI_BASE_URL=你的服务商 base url
OPENAI_API_KEY=你的 key
OPENAI_MODEL=你的模型名
```

重启服务：

```bash
docker compose --env-file .env -f deploy/docker-compose.yml up -d --build
```

再跑：

```bash
docker run --rm agentforge-agentctl \
  run \
  --addr host.docker.internal:8080 \
  --prompt "用系统设计视角解释 AgentForge 的运行链路"
```

---

## 6. 验证 Skill 动态加载

W5 已经实现 Skill 动态加载。worker 启动时会扫描镜像里的 `/skills`，每次 Run 根据 prompt 选几个相关 Skill 注入 LLM。

确认 `.env`：

```dotenv
SKILL_ENABLED=true
SKILL_TOP_K=3
```

运行：

```bash
docker run --rm agentforge-agentctl \
  run \
  --addr host.docker.internal:8080 \
  --prompt "帮我列出 sandbox 文件工具怎么用"
```

看 worker 日志：

```bash
docker compose --env-file .env -f deploy/docker-compose.yml logs worker
```

你应该能看到类似：

```text
skill selector loaded root=/skills skills=5 top_k=3
skills loaded count=1
```

这表示：

```text
用户 prompt
  -> RuleSelector 选择相关 SKILL.md
  -> Runner 把完整 Skill 内容加入 system message
  -> LLM
```

---

## 7. 验证 Tool / Sandbox

W3/W4 已经实现工具系统。为了最简单先用 `SANDBOX_DRIVER=memory` 验证工具 RPC。如果你要验证真正 Docker 隔离，再切 `SANDBOX_DRIVER=docker`。

### 7.1 列出工具

```bash
docker run --rm agentforge-agentctl \
  tool list \
  --addr host.docker.internal:8080
```

你会看到：

```text
bash
fs_read
fs_write
fs_list
http_fetch
```

### 7.2 执行一个工具

```bash
docker run --rm agentforge-agentctl \
  tool exec fs_write \
  --addr host.docker.internal:8080 \
  --args '{"path":"hello.txt","content":"hello from AgentForge\n"}'
```

再读回来：

```bash
docker run --rm agentforge-agentctl \
  tool exec fs_read \
  --addr host.docker.internal:8080 \
  --args '{"path":"hello.txt"}'
```

### 7.3 切换到 Docker Sandbox

如果你想验证更真实的隔离，把 `.env` 改成：

```dotenv
SANDBOX_DRIVER=docker
SANDBOX_WORKSPACE_ROOT=/tmp/agentforge
SANDBOX_WORKSPACE_HOST=/tmp/agentforge
```

重启：

```bash
docker compose --env-file .env -f deploy/docker-compose.yml up -d --build
```

注意：Docker Sandbox 依赖宿主机 `/var/run/docker.sock`，并要求 workspace 路径能被宿主 Docker daemon 访问。Windows / macOS Docker Desktop 如果遇到挂载问题，先退回 `SANDBOX_DRIVER=memory`。

---

## 8. 验证 RAG

W6 增加了 Postgres + pgvector RAG。它的作用是：

```text
本地文档
  -> agentctl rag ingest
  -> gateway
  -> chunking + hash embedding
  -> Postgres pgvector
  -> worker Run 前检索
  -> 以 <untrusted> context 注入 LLM
```

### 8.1 开启 RAG

编辑 `.env`：

```dotenv
RAG_ENABLED=true
POSTGRES_DSN=postgres://agentforge:agentforge@postgres:5432/agentforge?sslmode=disable
RAG_EMBED_DIM=384
RAG_TOP_K=5
RAG_TENANT_ID=default
RAG_MIN_SCORE=0
```

重启：

```bash
docker compose --env-file .env -f deploy/docker-compose.yml up -d --build
```

查看 gateway/worker 日志：

```bash
docker compose --env-file .env -f deploy/docker-compose.yml logs gateway worker
```

看到类似：

```text
rag service enabled dim=384 top_k=5
rag retriever enabled tenant=default top_k=5
```

说明 RAG 已连接 Postgres 并初始化 schema。

### 8.2 导入 README

因为 CLI 跑在容器里，要把当前目录挂进去：

```bash
docker run --rm \
  -v "$PWD":/work \
  -w /work \
  agentforge-agentctl \
  rag ingest \
  --addr host.docker.internal:8080 \
  --path README.md \
  --tenant default \
  --source README.md
```

成功时输出：

```text
[rag] tenant=default source=README.md chunks=...
<chunk_id_1>
<chunk_id_2>
...
```

### 8.3 单独查询 RAG

```bash
docker run --rm agentforge-agentctl \
  rag query \
  --addr host.docker.internal:8080 \
  --query "W5 skill selector 怎么工作" \
  --tenant default \
  --top-k 5
```

你会看到分数、source、chunk id 和内容预览。

想看完整 chunk：

```bash
docker run --rm agentforge-agentctl \
  rag query \
  --addr host.docker.internal:8080 \
  --query "W5 skill selector 怎么工作" \
  --tenant default \
  --top-k 3 \
  --raw
```

### 8.4 让正常 Run 自动使用 RAG

```bash
docker run --rm agentforge-agentctl \
  run \
  --addr host.docker.internal:8080 \
  --prompt "根据项目文档解释 W5 skill selector"
```

看 worker 日志：

```bash
docker compose --env-file .env -f deploy/docker-compose.yml logs worker
```

看到：

```text
rag context loaded tenant=default chunks=...
```

说明 worker 已经在 LLM 调用前检索了 RAG，并把结果作为 `<untrusted>` system context 注入。

---

## 9. 常用操作

### 9.1 查看所有服务状态

```bash
docker compose --env-file .env -f deploy/docker-compose.yml ps
```

### 9.2 查看日志

全部日志：

```bash
docker compose --env-file .env -f deploy/docker-compose.yml logs -f
```

只看 worker：

```bash
docker compose --env-file .env -f deploy/docker-compose.yml logs -f worker
```

只看 gateway：

```bash
docker compose --env-file .env -f deploy/docker-compose.yml logs -f gateway
```

### 9.3 重启

```bash
docker compose --env-file .env -f deploy/docker-compose.yml restart
```

### 9.4 停止但保留镜像

```bash
docker compose --env-file .env -f deploy/docker-compose.yml down
```

### 9.5 停止并清空 Redis/Postgres 数据

项目 Makefile 的 `down` 会带 `-v`，会清理 volume：

```bash
make down
```

或者直接：

```bash
docker compose --env-file .env -f deploy/docker-compose.yml down -v
```

注意：`down -v` 会删掉 RAG 已 ingest 的数据。

---

## 10. 两种启动模式怎么选

### 模式 A：新手验证链路

推荐配置：

```dotenv
LLM_PROVIDER=mock
SANDBOX_DRIVER=memory
RAG_ENABLED=false
```

适合：

- 第一次启动
- 不想配置 API key
- 只想验证 gateway / Redis / worker / streaming

### 模式 B：真实 LLM + Skill

推荐配置：

```dotenv
LLM_PROVIDER=openai
OPENAI_API_KEY=sk-your-key
SANDBOX_DRIVER=memory
SKILL_ENABLED=true
RAG_ENABLED=false
```

适合：

- 看真实模型回答
- 验证 Skill 动态注入
- 避免 sandbox 路径挂载问题

### 模式 C：真实 LLM + Tool + RAG

推荐配置：

```dotenv
LLM_PROVIDER=openai
OPENAI_API_KEY=sk-your-key
SANDBOX_DRIVER=docker
SKILL_ENABLED=true
RAG_ENABLED=true
```

适合：

- 完整 demo
- 展示 Agent runtime 的系统设计亮点
- 验证 sandbox / tool / RAG 联动

---

## 11. 对系统设计读者的理解地图

你可以这样理解每一层：

```text
client layer
  agentctl

edge layer
  gateway: gRPC + ACP, request admission, event fanout

coordination layer
  scheduler: worker register / heartbeat

async layer
  Redis Stream: task queue
  Redis Pub/Sub: run event channel

execution layer
  worker: Runner state machine
  sandbox: isolated tool execution

context layer
  history: mutable message store
  skill: prompt-time instruction loading
  rag: retrieved external knowledge

storage layer
  Redis: queue/history/events
  Postgres + pgvector: vector chunks
```

当前 W6 的真实执行顺序：

```text
1. agentctl run 发送 prompt
2. gateway 生成 run_id / trace_id
3. gateway XADD 到 Redis Stream
4. worker XREADGROUP 消费任务
5. worker 写 user message 到 history
6. worker 根据 prompt 选择 Skill
7. worker 根据 prompt 查询 RAG
8. worker 拼 system + skill + rag + history
9. worker 调 LLM provider
10. 如模型请求 tool，worker 执行 tool 并继续喂回 LLM
11. 如模型请求 subagent，worker 本地创建 child run 并把结果作为 tool result 返回
12. 如历史过长，worker 发布 COMPACTING 并折叠旧历史
13. worker 发布 token/state/done 到 Redis Pub/Sub
14. gateway 订阅事件并流式回传 agentctl
```

---

## 12. 常见问题

### Q1：`OPENAI_API_KEY is required`

你现在是：

```dotenv
LLM_PROVIDER=openai
```

但没有填 key。

解决：

```dotenv
LLM_PROVIDER=mock
```

或者填：

```dotenv
OPENAI_API_KEY=sk-your-key
```

### Q2：`agentctl` 连不上 `localhost:8080`

如果 `agentctl` 在 Docker 容器里运行，容器里的 `localhost` 是容器自己，不是你的宿主机。

macOS / Windows 用：

```bash
--addr host.docker.internal:8080
```

Linux 用：

```bash
--network host --addr localhost:8080
```

### Q3：RAG ingest 提示服务不可用

检查 `.env`：

```dotenv
RAG_ENABLED=true
```

然后重启：

```bash
docker compose --env-file .env -f deploy/docker-compose.yml up -d --build
```

再看 gateway 日志：

```bash
docker compose --env-file .env -f deploy/docker-compose.yml logs gateway
```

### Q4：Postgres 端口冲突

如果你本机已经有 Postgres 占用 `5432`，改 `deploy/docker-compose.yml`：

```yaml
ports:
  - "15432:5432"
```

容器内部 DSN 不用改，因为 gateway/worker 仍然通过 compose 网络访问 `postgres:5432`。

### Q5：Sandbox Docker 模式失败

先退回：

```dotenv
SANDBOX_DRIVER=memory
```

等主链路稳定后再排查 Docker socket 和 workspace mount。

### Q6：我没有 Go，能不能跑？

能。主服务和 CLI 都可以用 Docker 构建：

```bash
docker compose --env-file .env -f deploy/docker-compose.yml up -d --build
docker build -f build/Dockerfile --build-arg BIN=agentctl -t agentforge-agentctl .
```

如果你想在本机直接执行 `./bin/agentctl`，可以用 Makefile 构建；本机没有 Go 时，Makefile 会自动使用 `golang:1.22-alpine` Docker 镜像：

```bash
make proto
make build
```

### Q7：修改了 proto 后为什么本地 build 失败？

本地 Go build 需要先生成 `pkg/proto/gen`：

```bash
make proto
make build
```

Dockerfile 会自动跑 `buf generate`，所以 Docker Compose 构建通常不需要你手动处理 proto。

---

## 13. 验证 Multi-Agent / Pipeline

W7 增加了两个入口：

- Supervisor：模型可以调用 `dispatch_subagent`，worker 本地创建一个 child run。
- Pipeline：你手写一个 YAML DAG，gateway 按 step 顺序投递给 worker 执行。

### 13.1 开启 Supervisor

编辑 `.env`：

```dotenv
MULTI_AGENT_ENABLED=true
SUBAGENT_MAX_DEPTH=2
SUBAGENT_MAX_CHILDREN=4
SUBAGENT_TIMEOUT=2m
```

重启：

```bash
docker compose --env-file .env -f deploy/docker-compose.yml up -d --build
```

运行：

```bash
docker run --rm agentforge-agentctl \
  run --addr host.docker.internal:8080 \
  --prompt "派一个子 Agent 总结 README，再用一句话给我结论"
```

真实模型可能会调用 `dispatch_subagent`。如果你用 `LLM_PROVIDER=mock`，mock 不会主动 tool-call，但服务端配置和 tool schema 仍然会加载。

### 13.2 运行 Pipeline

项目内置了一个示例：

```text
examples/pipeline/readme-review.yaml
```

运行：

```bash
docker run --rm \
  -v "$PWD":/work \
  -w /work \
  agentforge-agentctl \
  pipeline run \
  --addr host.docker.internal:8080 \
  --file examples/pipeline/readme-review.yaml
```

输出会包含每个 step 的：

```text
STEP  ROLE  STATUS  RUN_ID  SUMMARY
```

系统设计视角可以理解为：

```text
pipeline YAML
  -> gateway 解析 DAG
  -> 按拓扑顺序投递每个 step 到 Redis Stream
  -> worker 执行 step
  -> gateway 收集 step 输出
  -> 后续 step 注入前序输出
```

### 13.3 验证上下文压缩

默认压缩阈值较高。演示时可以临时调小：

```dotenv
CONTEXT_COMPACT_ENABLED=true
CONTEXT_COMPACT_MAX_CHARS=1200
CONTEXT_COMPACT_KEEP_HEAD=2
CONTEXT_COMPACT_KEEP_TAIL=4
```

当可见历史超过阈值，worker 日志会出现：

```text
COMPACTING
```

history 中会出现一条带 `compacted=true` tag 的摘要消息。

---

## 14. 一条完整 demo 脚本

下面是一套偏稳妥的演示脚本，适合给别人展示：

```bash
# 1. 准备配置
cp .env.example .env

# 2. 建议手动编辑 .env：
#    LLM_PROVIDER=mock
#    SANDBOX_DRIVER=memory
#    RAG_ENABLED=true

# 3. 启动服务
docker compose --env-file .env -f deploy/docker-compose.yml up -d --build

# 4. 构建 Docker 版 CLI
docker build -f build/Dockerfile --build-arg BIN=agentctl -t agentforge-agentctl .

# 5. 跑一次基础 run
docker run --rm agentforge-agentctl \
  run --addr host.docker.internal:8080 \
  --prompt "用一句话介绍 AgentForge"

# 6. 导入 README 到 RAG
docker run --rm -v "$PWD":/work -w /work agentforge-agentctl \
  rag ingest --addr host.docker.internal:8080 \
  --path README.md --tenant default --source README.md

# 7. 查询 RAG
docker run --rm agentforge-agentctl \
  rag query --addr host.docker.internal:8080 \
  --query "W5 skill selector 怎么工作" \
  --tenant default --top-k 5

# 8. 再跑一次带 RAG 的 run
docker run --rm agentforge-agentctl \
  run --addr host.docker.internal:8080 \
  --prompt "根据项目文档解释 W5 skill selector"

# 9. 运行 W7 pipeline demo
docker run --rm -v "$PWD":/work -w /work agentforge-agentctl \
  pipeline run --addr host.docker.internal:8080 \
  --file examples/pipeline/readme-review.yaml
```

如果第 8 步用的是 `LLM_PROVIDER=mock`，回答仍然是 mock 文本，但 worker 日志会显示 RAG context 已加载。要看真实回答，把 `.env` 切到真实 OpenAI 兼容模型。

---

## 15. W8 服务拆分和 Hook 怎么启动

W8 之后，系统多了三个独立服务：

- `skilld`：负责读取 `skills/`，给 worker 返回命中的 Skill。
- `ragd`：负责 RAG 入库和查询，gateway/worker 都通过它访问 Postgres。
- `hookd`：负责执行 Hook，当前内置了安全提示、危险 bash 拒绝、模拟 secret 脱敏。

最小启动方式：

```dotenv
RAG_ENABLED=true
HOOK_ENABLED=true
SKILL_SERVICE_ADDR=skilld:8084
RAG_SERVICE_ADDR=ragd:8085
HOOK_SERVICE_ADDR=hookd:8083
```

然后启动：

```bash
docker compose --env-file .env -f deploy/docker-compose.yml up -d --build
```

查看 Hook：

```bash
./bin/agentctl hook list --addr localhost:8083
```

测试危险 bash 拒绝：

```bash
./bin/agentctl hook run \
  --addr localhost:8083 \
  --event PreToolUse \
  --file examples/hooks/pretool_bash.json
```

如果看到：

```text
allowed=false
```

说明 Hook 生效。

查看 scheduler leader：

```bash
./bin/agentctl scheduler leader --addr localhost:8081
```

选择一个 worker：

```bash
./bin/agentctl scheduler pick --addr localhost:8081 --run-id demo
```

W8 当前已经接入真实 `wazero` WASI hook 和 etcd election。`hookd` 会同时加载规则 hook 与 `type=wasm` manifest；`scheduler leader` 读取 etcd election 结果，`scheduler pick` 只有 leader 执行选择，非 leader 会返回当前 leader 信息。Redis Stream 主消费链路仍保持 W1-W7 的抢占式消费方式，`Pick` 是 W8 先落地的调度控制面。

示例 wasm hook 的源码在 `hooks/wasm_enterprise_safety.go`，已生成的二进制在 `hooks/wasm_enterprise_safety.wasm`。需要重新生成时执行：

```bash
docker run --rm -v "$PWD:/src" -w /src tinygo/tinygo:0.33.0 \
  tinygo build -target=wasi -tags tinygo_wasm_hook \
  -o hooks/wasm_enterprise_safety.wasm hooks/wasm_enterprise_safety.go
```
