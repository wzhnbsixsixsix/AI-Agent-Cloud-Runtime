// Generated from ../api/controlplane.openapi.yaml by `npm run generate:api`.
export type AgentStatus = 'provisioning' | 'running' | 'stopped' | 'failed'
export interface Agent { id: string; name: string; role: string; systemPrompt: string; model: string; image: string; cpuQuotaUs: number; memoryMb: number; pidsLimit: number; tools: string[]; workspacePolicy: 'retain' | 'delete'; status: AgentStatus; volumeName: string; containerId: string; lastError?: string; createdAt: string; updatedAt: string }
export interface AgentRun { runId: string; agentId: string; traceId: string; prompt: string; status: string; error?: string; summary?: string; createdAt: string; updatedAt: string }
export interface WorkspaceEntry { path: string; name: string; directory: boolean; size: number }
export interface CreateAgentInput { name: string; role: string; systemPrompt: string; model?: string; image?: string; cpuQuotaUs?: number; memoryMb?: number; pidsLimit?: number; tools?: string[]; workspacePolicy?: 'retain' | 'delete' }
