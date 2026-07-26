import type { CreateAgentInput } from './api.generated'

export type AgentModel = NonNullable<CreateAgentInput['model']>

export const DEFAULT_AGENT_MODEL: AgentModel = 'glm-4.7-flash'

export const AGENT_MODEL_OPTIONS: Array<{ value: AgentModel; label: string }> = [
  { value: 'glm-4.7-flash', label: '智谱 GLM-4.7 Flash' },
  { value: 'Qwen/Qwen3.6-27B', label: 'ModelScope · Qwen3.6 27B' },
]
