# 企业研发运维中台实例计划

这份文档描述一个基于 AgentForge 的企业内中台 demo。它应该放在 fork 或分支中，不写入 runtime `main`。

## 仓库拆分

### 仓库 1：Runtime Main

主仓库只保留可复用基础能力：

- `RunAgent`、ACP、gRPC、queue、worker、sandbox、history
- Skill/RAG/Hook 服务
- Scheduler/discovery
- Observability 和 bench

主仓库不应该：

- import Lark SDK
- 调用 `lark-cli`
- 写死企业工单、审批、IM、Base 等业务流程

### 仓库 2：企业中台实例

建议仓库名：

```text
AgentForge-Enterprise-Ops-Copilot
```

建议分支名：

```text
demo/lark-ops-center
```

这个 fork 可以接入 `lark-cli` / 飞书 OpenAPI，做一个研发运维智能工单中台。

## 产品场景

员工在飞书里提交研发或运维问题，系统自动检索项目文档、Runbook、事故复盘、发布记录，然后派多个 Agent 分析并生成结构化处理建议。

示例输入：

```text
支付服务 10:20 后错误率升高，请排查可能原因，并给出低风险处理建议。
```

## 推荐流程

1. Lark bot 收到消息，或 CLI 模拟一条飞书消息。
2. demo service 调用 AgentForge `RunAgent` 或 `RunPipeline`。
3. RAG 检索内部文档、Runbook、事故复盘。
4. Skill selector 加载回复规范、回滚规范、排障规范。
5. Supervisor 或 Pipeline 派发角色：
   - `log-analyst`
   - `runbook-agent`
   - `release-reviewer`
   - `risk-reviewer`
6. 汇总结果回写飞书消息或文档。
7. 可选：创建 Lark Base 工单记录、Lark Task、审批草稿。

## Fork 建议目录

```text
cmd/larkbot/
internal/lark/
examples/lark/docs/
examples/lark/skills/
examples/lark/pipelines/incident_triage.yaml
docs/ENTERPRISE_OPS_WALKTHROUGH.md
```

## Runtime 调用契约

实例仓库应该调用现有公共入口：

- `agentctl run`
- `agentctl pipeline run`
- `AgentService.RunAgent`
- `AgentService.RunPipeline`
- `agentctl rag ingest/query`

实例仓库不应该修改：

- `RunAgent` wire shape
- ACP frame format
- Tool schema
- Skill/RAG/Hook service contracts

## 安全边界

所有影响生产的动作默认只生成建议或审批草稿。demo 不直接执行生产变更、回滚命令或破坏性操作。

## 面试讲法

可以这样说：

> 主仓库是通用 AI Agent Runtime，负责调度、隔离、工具执行、RAG、Skill、Hook、Multi-Agent 和可观测性。企业中台实例是一个 fork，它调用 runtime 的 CLI/gRPC，并接入 Lark 作为企业协同入口。这样既能展示底层 AI Infra，也能展示 runtime 如何落到真实业务中台。
