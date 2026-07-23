package controlplane

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	_ "github.com/jackc/pgx/v5/stdlib"
)

type Store struct{ db *sql.DB }

func OpenStore(ctx context.Context, dsn string) (*Store, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(8)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	s := &Store{db: db}
	if err := s.EnsureSchema(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) EnsureSchema(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS agents (
			id TEXT PRIMARY KEY, name TEXT NOT NULL UNIQUE, role TEXT NOT NULL, system_prompt TEXT NOT NULL,
			model TEXT NOT NULL, image TEXT NOT NULL, cpu_quota_us BIGINT NOT NULL, memory_mb BIGINT NOT NULL,
			pids_limit BIGINT NOT NULL, tools_json JSONB NOT NULL DEFAULT '[]', workspace_policy TEXT NOT NULL,
			status TEXT NOT NULL, volume_name TEXT NOT NULL, container_id TEXT NOT NULL DEFAULT '',
			last_error TEXT NOT NULL DEFAULT '', created_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		);
		CREATE TABLE IF NOT EXISTS agent_runs (
			run_id TEXT PRIMARY KEY, agent_id TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE, trace_id TEXT NOT NULL,
			prompt TEXT NOT NULL, status TEXT NOT NULL, error TEXT NOT NULL DEFAULT '', summary TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		);
		CREATE UNIQUE INDEX IF NOT EXISTS agent_runs_one_active_idx ON agent_runs(agent_id) WHERE status = 'running';
		CREATE INDEX IF NOT EXISTS agent_runs_created_idx ON agent_runs(created_at DESC);
	`)
	return err
}

func (s *Store) Create(ctx context.Context, a AgentSpec) error {
	tools, _ := json.Marshal(a.Tools)
	_, err := s.db.ExecContext(ctx, `INSERT INTO agents
		(id,name,role,system_prompt,model,image,cpu_quota_us,memory_mb,pids_limit,tools_json,workspace_policy,status,volume_name,container_id,last_error)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
		a.ID, a.Name, a.Role, a.SystemPrompt, a.Model, a.Image, a.CPUQuotaUS, a.MemoryMB, a.PidsLimit, tools, a.WorkspacePolicy, a.Status, a.VolumeName, a.ContainerID, a.LastError)
	return err
}

func (s *Store) UpdateRuntime(ctx context.Context, id, status, containerID, lastError string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE agents SET status=$2, container_id=$3, last_error=$4, updated_at=now() WHERE id=$1`, id, status, containerID, lastError)
	return err
}

func scanAgent(row interface{ Scan(...any) error }) (AgentSpec, error) {
	var a AgentSpec
	var tools []byte
	err := row.Scan(&a.ID, &a.Name, &a.Role, &a.SystemPrompt, &a.Model, &a.Image, &a.CPUQuotaUS, &a.MemoryMB, &a.PidsLimit, &tools, &a.WorkspacePolicy, &a.Status, &a.VolumeName, &a.ContainerID, &a.LastError, &a.CreatedAt, &a.UpdatedAt)
	if err == nil {
		_ = json.Unmarshal(tools, &a.Tools)
	}
	return a, err
}

const agentColumns = `id,name,role,system_prompt,model,image,cpu_quota_us,memory_mb,pids_limit,tools_json,workspace_policy,status,volume_name,container_id,last_error,created_at,updated_at`

func (s *Store) Get(ctx context.Context, id string) (AgentSpec, error) {
	return scanAgent(s.db.QueryRowContext(ctx, `SELECT `+agentColumns+` FROM agents WHERE id=$1`, id))
}
func (s *Store) List(ctx context.Context) ([]AgentSpec, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+agentColumns+` FROM agents ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AgentSpec
	for rows.Next() {
		a, e := scanAgent(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
func (s *Store) Delete(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM agents WHERE id=$1`, id)
	return err
}

func (s *Store) CreateRun(ctx context.Context, r AgentRun) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO agent_runs(run_id,agent_id,trace_id,prompt,status) VALUES($1,$2,$3,$4,$5)`, r.RunID, r.AgentID, r.TraceID, r.Prompt, r.Status)
	return err
}
func (s *Store) FinishRun(ctx context.Context, id, status, summary, runErr string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE agent_runs SET status=$2,summary=$3,error=$4,updated_at=now() WHERE run_id=$1`, id, status, summary, runErr)
	return err
}
func (s *Store) ListRuns(ctx context.Context, agentID string) ([]AgentRun, error) {
	q := `SELECT run_id,agent_id,trace_id,prompt,status,error,summary,created_at,updated_at FROM agent_runs`
	args := []any{}
	if agentID != "" {
		q += ` WHERE agent_id=$1`
		args = append(args, agentID)
	}
	q += ` ORDER BY created_at DESC LIMIT 100`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AgentRun{}
	for rows.Next() {
		var r AgentRun
		if err := rows.Scan(&r.RunID, &r.AgentID, &r.TraceID, &r.Prompt, &r.Status, &r.Error, &r.Summary, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func (s *Store) GetRun(ctx context.Context, id string) (AgentRun, error) {
	var r AgentRun
	err := s.db.QueryRowContext(ctx, `SELECT run_id,agent_id,trace_id,prompt,status,error,summary,created_at,updated_at FROM agent_runs WHERE run_id=$1`, id).Scan(&r.RunID, &r.AgentID, &r.TraceID, &r.Prompt, &r.Status, &r.Error, &r.Summary, &r.CreatedAt, &r.UpdatedAt)
	return r, err
}
func IsConflict(err error) bool {
	return err != nil && (fmt.Sprintf("%v", err) != "" && (contains(err.Error(), "agent_runs_one_active_idx") || contains(err.Error(), "duplicate key")))
}
func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
