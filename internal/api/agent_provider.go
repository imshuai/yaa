package api

import (
	"context"

	"github.com/imshuai/yaa/internal/agent"
)

// AgentProvider 由 Agent Manager 实现，注入到 API Server。
type AgentProvider interface {
	HandleTurn(ctx context.Context, agentID string, req agent.TurnRequest) (agent.TurnResult, error)
	Get(id string) (agent.Info, error)
}
