package savedview

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery/domain"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery/ports"
	"strings"
)

// Executor is provided per call by the existing SQLite owner. The adapter never
// opens/closes a DB or begins/commits a transaction; root-handle selection stays
// in SQLiteRepository.executor. No handle is exposed by SavedViewRepository.
type Executor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}
type Resolver func(context.Context) (Executor, error)
type repository struct{ resolve Resolver }

func New(resolve Resolver) ports.SavedViewRepository { return &repository{resolve: resolve} }

func (r *repository) CreateSavedView(ctx context.Context, view domain.SavedView) error {
	if r == nil || r.resolve == nil {
		return errors.New("SQLite repository is not configured")
	}
	payload, err := json.Marshal(view)
	if err != nil {
		return fmt.Errorf("encode delivery saved view: %w", err)
	}
	executor, err := r.resolve(ctx)
	if err != nil {
		return err
	}
	_, err = executor.ExecContext(ctx, `INSERT INTO iotd_delivery_saved_views (id, payload, updated_at) VALUES (?, ?, ?)`, view.ID, string(payload), view.UpdatedAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"))
	if err != nil {
		return fmt.Errorf("insert delivery saved view: %w", err)
	}
	return nil
}
func (r *repository) ListSavedViews(ctx context.Context, owner string) ([]domain.SavedView, error) {
	if r == nil || r.resolve == nil {
		return nil, errors.New("SQLite repository is not configured")
	}
	executor, err := r.resolve(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := executor.QueryContext(ctx, `SELECT payload FROM iotd_delivery_saved_views ORDER BY updated_at DESC, id ASC`)
	if err != nil {
		return nil, fmt.Errorf("list delivery saved views: %w", err)
	}
	defer rows.Close()
	// Read rows before decoding, preserving the original error/iteration order.
	payloads := make([]string, 0)
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, fmt.Errorf("scan delivery saved views: %w", err)
		}
		payloads = append(payloads, payload)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate delivery saved views: %w", err)
	}
	owner = strings.TrimSpace(owner)
	views := make([]domain.SavedView, 0, len(payloads))
	for _, payload := range payloads {
		var view domain.SavedView
		if err := json.Unmarshal([]byte(payload), &view); err != nil {
			return nil, fmt.Errorf("decode delivery saved view: %w", err)
		}
		if owner == "" || view.Owner == owner {
			views = append(views, view)
		}
	}
	return views, nil
}
