import { describe, expect, it } from 'vitest'
import type { AgentRun } from './api'
import { findActiveRun } from './run-recovery'

const run = (runId: string, status: string): AgentRun => ({
  runId, agentId: 'agent-1', traceId: 'trace-1', prompt: 'work', status,
  createdAt: '2026-08-05T00:00:00Z', updatedAt: '2026-08-05T00:00:00Z',
})

describe('findActiveRun', () => {
  it('restores the Agent run that is still running after a page refresh', () => {
    expect(findActiveRun([run('completed-run', 'completed'), run('active-run', 'running')])?.runId).toBe('active-run')
  })

  it('does not reopen a terminal run', () => {
    expect(findActiveRun([run('completed-run', 'completed'), run('failed-run', 'failed')])).toBeUndefined()
  })
})
