# ACP — Agent Control Protocol（v1）

> AgentForge 自研的双向流协议，对照 gRPC over HTTP/2 在 *agent 流式输出* 这个特定场景上做减法。
> 工作在 **裸 TCP** 之上，单连接单会话（W2 阶段不做多路复用）。

---

## 1. 帧布局

固定头 12 字节 + uvarint 长度 + payload：

```
 0                   1                   2                   3
 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
+---------------+---------------+---------------+---------------+
|     0xAC      |   version=1   |     type      |     flags     |
+---------------+---------------+---------------+---------------+
|                                                               |
+                       seq (uint64, big-endian)                +
|                                                               |
+---------------+---------------+---------------+---------------+
|              len  (uvarint, 1~10 bytes)                       |
+---------------------------------------------------------------+
|                       payload (len bytes)                     |
+---------------------------------------------------------------+
```

- **magic**：固定 `0xAC`，单字节（"A" of AgentForge）。错则立即关闭连接。
- **version**：当前 `0x01`，不一致回 `ERROR{code="version_mismatch"}` 后关连接。
- **type**：参见下表。
- **flags**：bit 位
  - `0x01` END_STREAM：最后一帧（DONE/ERROR 后必须置位）
  - `0x02` COMPRESSED：保留位，W2 不启用
- **seq**：`server→client` 的事件帧使用单调递增 seq（断线续传依据）；其余帧为 0。
- **len**：payload 字节长度，使用 protobuf 风格 unsigned varint，最大 16 MiB（超限关连接）。
- **payload**：按 type 决定编码，见下文。

## 2. 帧类型

| 值     | 名称       | 方向 | payload 编码                  |
|--------|-----------|------|------------------------------|
| `0x01` | HELLO     | C→S  | JSON `Hello`                 |
| `0x02` | HELLO_ACK | S→C  | JSON `HelloAck`              |
| `0x10` | RUN       | C→S  | protobuf `RunRequest`        |
| `0x20` | EVENT     | S→C  | protobuf `RunEvent`          |
| `0x30` | PING      | both | 8 bytes nonce（可 0）         |
| `0x31` | PONG      | both | 回显对端 PING 的 nonce        |
| `0x40` | RESUME    | C→S  | JSON `Resume`                |
| `0x50` | CLOSE     | both | JSON `Close`                 |
| `0xF0` | ERROR     | S→C  | JSON `Error`                 |

## 3. 控制帧 JSON 结构

```jsonc
// HELLO
{ "client_version": "agentforge-1.0", "run_id": "", "user_id": "alice" }

// HELLO_ACK
{ "server_version": "agentforge-1.0", "run_id": "01HXXXX...", "trace_id": "01HXXX..." }

// RESUME
{ "run_id": "01HXXX...", "last_seq": 42 }

// CLOSE
{ "code": "client_done", "message": "" }

// ERROR
{ "code": "version_mismatch", "message": "want v1 got v2", "retriable": false }
```

## 4. 会话流程

### 4.1 正常会话

```
C: HELLO          ─►
                  ◄─ S: HELLO_ACK (run_id, trace_id)
C: RUN(prompt)    ─►
                  ◄─ S: EVENT seq=1 (state RUNNING)
                  ◄─ S: EVENT seq=2 (token "hello")
                  ◄─ S: EVENT seq=3 (token " world")
                  ◄─ S: EVENT seq=4 END_STREAM (done)
C: CLOSE          ─►
```

### 4.2 心跳

- 客户端每 15s 发 PING，服务端立即回 PONG。
- 任意一端 30s 内未收到对端任何帧（含 PONG / EVENT），关连接。

### 4.3 断线续传

```
[网络抖动，连接断开]

C 重连 → HELLO { run_id: "01HX...", resume: true } —— 即 RESUME 帧
                  ◄─ S: HELLO_ACK
C: RESUME { last_seq: 3 }
                  ◄─ S: EVENT seq=4 (token "!")
                  ◄─ S: EVENT seq=5 END_STREAM (done)
```

实现细节：服务端把每条 EVENT 同步写入 Redis ZSet `acp:events:{run_id}`
(score=seq, member=raw_payload_bytes, TTL=1h)。RESUME 时按 score>last_seq 全部回放，
随后接续 Pub/Sub 实时事件（若 run 仍在进行）。

## 5. 错误处理

- 解析错误（magic 不匹配 / len 超限 / payload 解码失败）→ 直接关连接，不发 ERROR（怀疑攻击）。
- 业务错误（run 失败 / resume 缺数据）→ 发 ERROR 帧 + END_STREAM，再关连接。
- 服务端关停时发 `CLOSE{code="server_shutdown"}`，给客户端 5s 优雅退出窗口。

## 6. 与 gRPC 的对比要点

| 维度            | ACP                    | gRPC over HTTP/2          |
|----------------|------------------------|---------------------------|
| 握手 RTT       | 1 (TCP) + 0 (HELLO 与 RUN 可合并) | 1 (TCP) + 1 (HTTP/2 settings) |
| 帧头           | 12B + uvarint          | 9B + HEADERS frame        |
| 双向流         | 原生                    | 原生                       |
| 断线续传        | 内建（seq + ZSet）       | 需应用层 + interceptor     |
| 多路复用        | ❌（W2 不做）            | ✅ stream_id              |
| 流控           | TCP 自带                | HTTP/2 window update      |
| 工程复杂度      | 低（~600 LOC）           | 高（依赖 grpc 生态）        |

> 设计取舍：ACP 用单连接换简单和延迟，多路复用 / 复杂控制面留给 gRPC 内部链路。

## 7. Agent 协作扩展（规划）

ACP v1 当前定义的是 client ↔ gateway 的 Run/Event 流。多 Agent 场景将在保持该语义不变的基础上，增加经 **ACP Collaboration Gateway** 路由的 task/event contract；Agent 之间不直接建立点对点连接。

- **gRPC**：Agent 调用 RAG、OCR、Search、Memory、SQL 等基础能力服务。
- **ACP**：Agent 投递、订阅和消费协作任务，以及接收任务进度、结果和失败事件。
- **Redis**：协作任务、状态和事件的持久化与回放来源；容器内存不是可靠消息副本。

协作结果至少应包含下列字段（具体 protobuf/frame 类型在实现阶段确定）：

```json
{
  "task_id": "task-001",
  "parent_task_id": "run-123",
  "sender_agent_id": "researcher-a",
  "receiver_agent_id": "writer-b",
  "type": "knowledge_result",
  "status": "completed",
  "trace_id": "trace-456",
  "idempotency_key": "…",
  "payload": {
    "summary": "Transformer 论文发表于 2017 年。",
    "confidence": 0.93,
    "citations": [{"source": "Attention Is All You Need", "chunk_id": "…", "score": 0.91}]
  }
}
```

完整检索文本或其他大体积内容使用 artifact ID 引用受控存储，不直接写入 ACP payload。Gateway 负责认证授权、目标路由、离线暂存、幂等去重、重试、审计和基于 sequence 的事件回放。
