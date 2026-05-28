# AI Agent Cloud Runtime

一个面向 **AI Infra / Agent Runtime** 的可执行平台，而不是普通 CRUD 项目。

## 项目定位

> 类 Devin / Manus 的云端 AI Agent 执行平台

用户输入任务（如“分析 GitHub 项目”“改代码”“部署网站”“生成 PPT”）后，系统自动完成：

1. 创建隔离 sandbox
2. 选择并编排主 Agent / Sub-Agent
3. 路由技能（skills）
4. 检索上下文（RAG + memory）
5. 云端执行并流式返回结果

## 核心架构

### 1) Gateway (Go)
- HTTP API / WebSocket
- 用户鉴权
- Agent Session 管理
- 流式输出

### 2) Runtime Scheduler
- Agent 与 sandbox 分配
- 任务生命周期管理
- worker pool / priority queue
- retry / timeout kill

### 3) Sandbox Service
- 动态创建 Docker
- workspace 与文件系统隔离
- 命令执行与权限限制
- 多语言运行时支持（Python / Node / Go）

### 4) Multi-Agent Orchestrator
- 主 Agent 拆分子任务
- Sub-Agent 并行协作
- context compression / memory merge
- hook 与异步事件驱动

### 5) Skill System
- skill 元数据解析
- embedding 建索引
- skill router + function calling

示例目录：

```txt
skills/
 ├── git.skill
 ├── deploy.skill
 └── summarize.skill
```

示例 skill：

```txt
name: deploy
description: deploy docker app to cloud

steps:
- build docker
- push image
```

### 6) Conversation Rewrite Layer
- 支持“基于历史消息的补丁式改写”
- 可定向重写指定 message
- 重新注入上下文，保证会话一致性

示例：

```json
{
  "message_id": "xxx",
  "patch": "formal_style"
}
```

### 7) RAG Memory System（分层记忆）
- 短期记忆：Redis
- 长期记忆：SQLite + embedding
- 压缩记忆：summary memory
- workspace scoped memory

### 8) Hook System
- `BeforeAgentRun`
- `AfterToolCall`
- `OnMemoryWrite`
- `OnSandboxCreate`

支持 prompt 注入、上下文改写、审计与日志。

### 9) Agent RPC Protocol

```json
{
  "type": "tool_call",
  "agent_id": "agent_1",
  "tool": "search",
  "payload": {}
}
```

协议目标：
- streaming
- event
- interrupt / cancel
- retry

## 里程碑（MVP）

- [ ] Gateway + Session + Streaming
- [ ] Scheduler + Worker Pool + Retry/Timeout
- [ ] Sandbox Docker 执行链路
- [ ] Multi-Agent 编排与上下文压缩
- [ ] Skill 加载 / 检索 / 路由
- [ ] 分层记忆（Redis + SQLite + RAG）
- [ ] Hook 与 Agent RPC 协议

## 简历描述（可直接使用）

- Built a cloud-native multi-agent runtime system in Go supporting sandboxed task execution and dynamic skill orchestration
- Designed a custom agent communication protocol supporting streaming events, tool calling, hooks, and asynchronous task scheduling
- Implemented hierarchical memory architecture with Redis + SQLite + RAG-based context retrieval
- Developed isolated Docker sandbox infrastructure for secure code execution and workspace separation
