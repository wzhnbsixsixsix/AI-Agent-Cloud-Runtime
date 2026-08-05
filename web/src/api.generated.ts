// Compact UI aliases maintained alongside ../api/controlplane.openapi.yaml.
export type AgentStatus = 'provisioning' | 'running' | 'stopped' | 'failed'
export interface Agent { id: string; name: string; role: string; systemPrompt: string; model: string; image: string; cpuQuotaUs: number; memoryMb: number; pidsLimit: number; tools: string[]; workspacePolicy: 'retain' | 'delete'; status: AgentStatus; volumeName: string; containerId: string; lastError?: string; createdAt: string; updatedAt: string }
export interface AgentRun { runId: string; agentId: string; traceId: string; prompt: string; status: string; error?: string; summary?: string; createdAt: string; updatedAt: string }
export interface BatchDeleteAgentsResult { deletedIds: string[]; failed: AgentDeleteFailure[] }
export interface AgentDeleteFailure { agentId?: string; message: string }
export interface WorkspaceEntry { path: string; name: string; directory: boolean; size: number }
export interface CreateAgentInput { name: string; role: string; systemPrompt: string; model?: 'glm-4.7-flash' | 'Qwen/Qwen3.5-35B-A3B'; image?: string; cpuQuotaUs?: number; memoryMb?: number; pidsLimit?: number; tools?: string[]; workspacePolicy?: 'retain' | 'delete' }
// The dashboard needs this status response before opening any Agent page.
export interface ControlPlaneStatus { status: 'ok' | 'degraded'; activeRuns: number; checks: Record<string, 'ok' | 'failed'> }
