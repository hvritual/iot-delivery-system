package audit

import (
	"context"
	"errors"

	"github.com/hvritual/yunka.io/framework/operation"
	"github.com/hvritual/yunka.io/gateway/authz"
	"github.com/hvritual/yunka.io/pkg/operationplan"
)

// NewRecordingExecutor decorates the single OperationPlan executor shared by
// REST, generated gRPC, and MCP. Audit persistence failure never changes the
// stable denied response category.
func NewRecordingExecutor(delegate operation.Executor, recorder *SecurityRecorder) (operation.Executor, error) {
	if delegate == nil {
		return nil, operation.ErrExecutorUnavailable
	}
	if recorder == nil {
		return nil, errors.New("security audit recorder is required")
	}
	return recordingExecutor{delegate: delegate, recorder: recorder}, nil
}

type recordingExecutor struct {
	delegate operation.Executor
	recorder *SecurityRecorder
}

func (executor recordingExecutor) Execute(ctx context.Context, plan operationplan.Plan, input any, invoke operation.Invoker) (any, error) {
	if invoke == nil {
		return executor.delegate.Execute(ctx, plan, input, nil)
	}
	invoked := false
	value, err := executor.delegate.Execute(ctx, plan, input, func(callContext context.Context) (any, error) {
		invoked = true
		return invoke(callContext)
	})
	if err != nil && authz.IsDenied(err) {
		_ = executor.recorder.RecordAuthorizationDenied(ctx, plan.OperationID)
	} else if err != nil && invoked && plan.Execution.Transaction == "local" {
		_ = executor.recorder.RecordApplicationRollback(ctx, plan.OperationID)
	}
	return value, err
}

func (executor recordingExecutor) ExecuteChild(ctx context.Context, plan operationplan.Plan, input any, invoke operation.Invoker) (any, error) {
	return operation.ExecuteChild(ctx, executor.delegate, plan, input, invoke)
}
