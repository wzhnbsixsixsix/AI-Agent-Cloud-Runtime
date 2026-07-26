import { describe, expect, it } from 'vitest'
import { AGENT_MODEL_OPTIONS, DEFAULT_AGENT_MODEL } from './models'

describe('Agent model options', () => {
  it('keeps GLM as the default and exposes ModelScope Qwen', () => {
    expect(DEFAULT_AGENT_MODEL).toBe('glm-4.7-flash')
    expect(AGENT_MODEL_OPTIONS.map(option => option.value)).toEqual([
      'glm-4.7-flash',
      'Qwen/Qwen3.6-27B',
    ])
  })
})
