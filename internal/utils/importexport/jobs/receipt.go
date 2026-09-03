package jobs

import (
	"context"
	"database/sql"
	"fmt"
)

// ImportReceiptExecutor is satisfied by *sql.DB and *sql.Tx.
type ImportReceiptExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

// ClaimImportReceipt records an import application. It must be called from the
// same transaction that applies the domain import. A false result means a
// previous attempt has already committed that application.
func ClaimImportReceipt(
	ctx context.Context,
	executor ImportReceiptExecutor,
	jobID int64,
	service, workspaceID string,
) (bool, error) {
	if jobID <= 0 {
		return false, fmt.Errorf(
			"importexport jobs: invalid import receipt job id",
		)
	}

	if err := validateIdentity(service, workspaceID); err != nil {
		return false, err
	}

	result, err := executor.ExecContext(ctx, `
INSERT INTO importexport_job_import_receipt (job_id, service, workspace_id)
VALUES ($1, $2, $3)
ON CONFLICT (job_id, service, workspace_id) DO NOTHING`, jobID, service, workspaceID)
	if err != nil {
		return false, err
	}

	inserted, err := result.RowsAffected()
	if err != nil {
		return false, err
	}

	return inserted == 1, nil
}
