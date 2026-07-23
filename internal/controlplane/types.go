// Package controlplane implements the browser-facing management API for durable Agents.
package controlplane

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	AgentModel         = "glm-4.7-flash"
	StatusProvisioning = "provisioning"
	StatusRunning      = "running"
	StatusStopped      = "stopped"
	StatusFailed       = "failed"
)

type AgentSpec struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Role            string    `json:"role"`
	SystemPrompt    string    `json:"systemPrompt"`
	Model           string    `json:"model"`
	Image           string    `json:"image"`
	CPUQuotaUS      int64     `json:"cpuQuotaUs"`
	MemoryMB        int64     `json:"memoryMb"`
	PidsLimit       int64     `json:"pidsLimit"`
	Tools           []string  `json:"tools"`
	WorkspacePolicy string    `json:"workspacePolicy"`
	Status          string    `json:"status"`
	VolumeName      string    `json:"volumeName"`
	ContainerID     string    `json:"containerId"`
	LastError       string    `json:"lastError,omitempty"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

type AgentRun struct {
	RunID     string    `json:"runId"`
	AgentID   string    `json:"agentId"`
	TraceID   string    `json:"traceId"`
	Prompt    string    `json:"prompt"`
	Status    string    `json:"status"`
	Error     string    `json:"error,omitempty"`
	Summary   string    `json:"summary,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type CreateAgentInput struct {
	Name            string   `json:"name"`
	Role            string   `json:"role"`
	SystemPrompt    string   `json:"systemPrompt"`
	Model           string   `json:"model"`
	Image           string   `json:"image"`
	CPUQuotaUS      int64    `json:"cpuQuotaUs"`
	MemoryMB        int64    `json:"memoryMb"`
	PidsLimit       int64    `json:"pidsLimit"`
	Tools           []string `json:"tools"`
	WorkspacePolicy string   `json:"workspacePolicy"`
}

func (in CreateAgentInput) Validate(defaultImage string) error {
	if len(strings.TrimSpace(in.Name)) < 2 || len(in.Name) > 80 {
		return errors.New("name must contain 2-80 characters")
	}
	if len(strings.TrimSpace(in.Role)) == 0 || len(in.Role) > 120 {
		return errors.New("role is required and must not exceed 120 characters")
	}
	if len(in.SystemPrompt) > 12000 {
		return errors.New("systemPrompt must not exceed 12000 characters")
	}
	if in.Model != "" && in.Model != AgentModel {
		return fmt.Errorf("only model %q is supported", AgentModel)
	}
	if in.Image != "" && in.Image != defaultImage {
		return fmt.Errorf("image %q is not allowed", in.Image)
	}
	if in.CPUQuotaUS < 0 || in.MemoryMB < 0 || in.PidsLimit < 0 {
		return errors.New("resource limits cannot be negative")
	}
	if in.WorkspacePolicy != "" && in.WorkspacePolicy != "retain" && in.WorkspacePolicy != "delete" {
		return errors.New("workspacePolicy must be retain or delete")
	}
	return nil
}

func normalizeCreate(in CreateAgentInput, defaultImage string) CreateAgentInput {
	in.Name = strings.TrimSpace(in.Name)
	in.Role = strings.TrimSpace(in.Role)
	if in.Image == "" {
		in.Image = defaultImage
	}
	if in.Model == "" {
		in.Model = AgentModel
	}
	if in.CPUQuotaUS == 0 {
		in.CPUQuotaUS = 50000
	}
	if in.MemoryMB == 0 {
		in.MemoryMB = 256
	}
	if in.PidsLimit == 0 {
		in.PidsLimit = 256
	}
	if in.WorkspacePolicy == "" {
		in.WorkspacePolicy = "retain"
	}
	return in
}
