import type { Agent, AgentRun, CreateAgentInput, WorkspaceEntry } from './api.generated'
export * from './api.generated'

async function request<T>(url: string, init?: RequestInit): Promise<T> { const res = await fetch(url, { headers: { 'Content-Type': 'application/json', ...(init?.headers ?? {}) }, ...init }); if (!res.ok) { const error = await res.json().catch(() => ({ message: res.statusText })); throw new Error(error.message ?? 'Request failed') } return res.status === 204 ? undefined as T : res.json() as Promise<T> }
export const api = {
  agents: () => request<Agent[]>('/api/v1/agents'), agent: (id: string) => request<Agent>(`/api/v1/agents/${id}`), createAgent: (input: CreateAgentInput) => request<Agent>('/api/v1/agents', { method: 'POST', body: JSON.stringify(input) }),
  agentAction: (id: string, action: 'start' | 'stop' | 'delete') => request<Agent | undefined>(`/api/v1/agents/${id}:${action}`, { method: 'POST' }),
  runs: (agentId?: string) => request<AgentRun[]>(agentId ? `/api/v1/agents/${agentId}/runs` : '/api/v1/runs'), startRun: (agentId: string, prompt: string) => request<AgentRun>(`/api/v1/agents/${agentId}/runs`, { method: 'POST', body: JSON.stringify({ prompt }) }),
  workspace: (id: string, path = '') => request<WorkspaceEntry[]>(`/api/v1/agents/${id}/workspace?path=${encodeURIComponent(path)}`), workspaceFile: (id: string, path: string) => request<{content: string}>(`/api/v1/agents/${id}/workspace/file?path=${encodeURIComponent(path)}`)
}
