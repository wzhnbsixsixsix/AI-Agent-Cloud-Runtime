import type { AgentRun } from './api'

// The API keeps a single persisted `running` Run per Agent. Selecting it here
// lets a fresh dashboard page reconnect to the SSE replay for that same Run.
export function findActiveRun(runs: AgentRun[] | undefined): AgentRun | undefined {
  return runs?.find(run => run.status === 'running')
}
