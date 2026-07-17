package sql

import (
	"context"
	"database/sql"
	"fmt"
)

// WithTx starts a transaction, creates sqlc queries via newQueries,
// runs callback, and guarantees rollback on error/panic.
func WithTx[Q any](
	ctx context.Context,
	db *sql.DB,
	newQueries func(tx *sql.Tx) *Q,
	clb func(tx *sql.Tx, queries *Q) error,
) (txErr error) {
	if db == nil {
		return ErrNilDB
	}
	if newQueries == nil {
		return fmt.Errorf("sqlcwrap: newQueries is nil")
	}
	if clb == nil {
		return fmt.Errorf("sqlcwrap: callback is nil")
	}

	tx, txErr := db.BeginTx(ctx, nil)
	if txErr != nil {
		return txErr
	}

	queries := newQueries(tx)

	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
		if txErr != nil {
			_ = tx.Rollback()
		}
	}()

	if txErr = clb(tx, queries); txErr != nil {
		return txErr
	}

	txErr = tx.Commit()
	if txErr != nil {
		return fmt.Errorf("failed to commit tx: %w", txErr)
	}

	return nil
}

// InTx starts a transaction using Client timeout policy.
func (c *Client) InTx(
	ctx context.Context,
	clb func(tx *sql.Tx) error,
) error {
	if c == nil {
		return ErrNilDB
	}
	if c.db == nil {
		return ErrNilDB
	}
	if clb == nil {
		return fmt.Errorf("sqlcwrap: callback is nil")
	}

	qctx, cancel := createContextWithTimeout(ctx, 0)
	defer cancel()

	tx, err := c.db.BeginTx(qctx, nil)
	if err != nil {
		return err
	}

	var txErr error
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
		if txErr != nil {
			_ = tx.Rollback()
		}
	}()

	if txErr = clb(tx); txErr != nil {
		return txErr
	}

	txErr = tx.Commit()
	if txErr != nil {
		return fmt.Errorf("failed to commit tx: %w", txErr)
	}

	return nil
}
