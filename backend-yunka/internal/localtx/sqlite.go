// Package localtx adapts a database/sql SQLite transaction to Yunka's local
// execution scope. It deliberately owns no business schema.
package localtx

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/hvritual/yunka.io/framework/execution"
)

type SQLiteFactory struct {
	database *sql.DB
}

func NewSQLiteFactory(database *sql.DB) *SQLiteFactory {
	return &SQLiteFactory{database: database}
}

func (factory *SQLiteFactory) Begin(ctx context.Context, mode execution.TransactionMode) (execution.UnitOfWork, error) {
	if factory == nil || factory.database == nil {
		return nil, errors.New("SQLite transaction factory is not configured")
	}
	options := &sql.TxOptions{ReadOnly: mode == execution.TransactionReadOnly}
	tx, err := factory.database.BeginTx(ctx, options)
	if err != nil {
		return nil, fmt.Errorf("begin SQLite transaction: %w", err)
	}
	return &sqliteUnitOfWork{transaction: tx}, nil
}

type sqliteUnitOfWork struct {
	transaction *sql.Tx
}

func (unit *sqliteUnitOfWork) Commit(context.Context) error {
	if unit == nil || unit.transaction == nil {
		return errors.New("SQLite transaction is not configured")
	}
	return unit.transaction.Commit()
}

func (unit *sqliteUnitOfWork) Rollback(context.Context) error {
	if unit == nil || unit.transaction == nil {
		return errors.New("SQLite transaction is not configured")
	}
	err := unit.transaction.Rollback()
	if errors.Is(err, sql.ErrTxDone) {
		return nil
	}
	return err
}

func (*sqliteUnitOfWork) Close() error { return nil }

func (unit *sqliteUnitOfWork) TransactionHandle() any {
	if unit == nil {
		return nil
	}
	return unit.transaction
}
