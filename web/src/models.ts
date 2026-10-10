import type { CreateAgentInput } from './api.generated'

export type AgentModel = NonNullable<CreateAgentInput['model']>

export const DEFAULT_AGENT_MODEL: AgentModel = 'Qwen/Qwen3.5-35B-A3B'

export const AGENT_MODEL_OPTIONS: Array<{ value: AgentModel; label: string }> = [
  { value: 'Qwen/Qwen3.5-35B-A3B', label: 'ModelScope · Qwen3.5 35B-A3B' },
]
