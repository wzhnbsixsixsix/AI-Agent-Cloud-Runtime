import { describe, expect, it } from 'vitest'
import { AGENT_MODEL_OPTIONS, DEFAULT_AGENT_MODEL } from './models'

describe('Agent model options', () => {
  it('uses ModelScope Qwen exclusively', () => {
    expect(DEFAULT_AGENT_MODEL).toBe('Qwen/Qwen3.5-35B-A3B')
    expect(AGENT_MODEL_OPTIONS.map(option => option.value)).toEqual([
      'Qwen/Qwen3.5-35B-A3B',
    ])
  })
})
