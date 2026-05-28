# AI Agent Cloud Runtime

An executable **AI Infra / Agent Runtime** platform, not a generic CRUD project.

## Positioning

> A cloud execution runtime for AI agents, inspired by Devin / Manus.

After a user submits tasks like “analyze a GitHub repository”, “modify code”, “deploy a website”, or “generate slides”, the system automatically:

1. Creates an isolated sandbox
2. Selects and orchestrates main agent and sub-agents
3. Routes skills
4. Retrieves context with RAG and memory
5. Executes in cloud runtime and streams results

## Core Architecture

### 1) Gateway (Go)
- HTTP API / WebSocket
- Authentication
- Agent session management
- Streaming output

### 2) Runtime Scheduler
- Agent and sandbox allocation
- Task lifecycle management
- Worker pool / priority queue
- Retry / timeout-based termination

### 3) Sandbox Service
- Dynamic Docker provisioning
- Workspace and filesystem isolation
- Command execution with permission limits
- Multi-language runtime support (Python / Node / Go)

### 4) Multi-Agent Orchestrator
- Main-agent task decomposition
- Parallel sub-agent collaboration
- Context compression (summarize sub-agent outputs)
- Memory merge (deduplicate and consolidate shared facts)
- Hooks and asynchronous event flow

### 5) Skill System
- Skill metadata parsing
- Embedding indexing
- Skill router + function calling

Skill file format convention: use **YAML** with `.skill` extension.

Example directory:

```plaintext
skills/
 ├── git.skill
 ├── deploy.skill
 └── summarize.skill
```

Example skill (YAML):

```yaml
name: deploy
description: deploy Docker app to cloud

steps:
- Build Docker image
- Push image
```

### 6) Conversation Rewrite Layer
- Patch-style rewrite based on previous assistant messages
- Targeted rewrite by message ID
- Context re-injection for conversation consistency

Example:

```json
{
  "message_id": "xxx",
  "patch": "formal_style"
}
```

### 7) RAG Memory System (Hierarchical Memory)
- Short-term memory: Redis
- Long-term memory: SQLite (MVP, embedding table + cosine search) / PostgreSQL + pgvector (production)
- Compressed memory: summary memory
- Workspace-scoped memory

### 8) Hook System
- `BeforeAgentRun`
- `AfterToolCall`
- `OnMemoryWrite`
- `OnSandboxCreate`

Supports prompt injection, context rewriting, auditing, and logging.

### 9) Agent RPC Protocol

```json
{
  "type": "tool_call",
  "agent_id": "agent_1",
  "tool": "search",
  "payload": {
    "query": "repo architecture",
    "top_k": 5
  }
}
```

Protocol goals:
- streaming
- event
- interrupt / cancel
- retry

## Milestones (MVP)

- [ ] Gateway + Session + Streaming
- [ ] Scheduler + Worker Pool + Retry/Timeout
- [ ] Sandbox Docker execution pipeline
- [ ] Multi-agent orchestration and context compression
- [ ] Skill loading / retrieval / routing
- [ ] Hierarchical memory (Redis + SQLite + RAG)
- [ ] Hook system and agent RPC protocol

## Resume Highlights (Use after project completion)

- Built a cloud-native multi-agent runtime system in Go supporting sandboxed task execution and dynamic skill orchestration
- Designed a custom agent communication protocol supporting streaming events, tool calling, hooks, and asynchronous task scheduling
- Implemented hierarchical memory architecture with Redis + SQLite + RAG-based context retrieval
- Developed isolated Docker sandbox infrastructure for secure code execution and workspace separation
