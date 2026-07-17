// Package importexport contains shared mechanical helpers for service import and export.
package importexport

import "errors"

const (
	DefaultMaxRows       = 1000
	DefaultMaxParameters = 60000
)

var ErrBatchCallbackRequired = errors.New("importexport: batch callback is required")

type BatchLimits struct {
	MaxRows       int
	MaxParameters int
}

var DefaultBatchLimits = BatchLimits{
	MaxRows:       DefaultMaxRows,
	MaxParameters: DefaultMaxParameters,
}

// BatchSize returns the safe number of rows for one SQL command.
func BatchSize(parametersPerRow int, limits BatchLimits) int {
	if parametersPerRow <= 0 {
		return 1
	}
	if limits.MaxRows <= 0 {
		limits.MaxRows = DefaultMaxRows
	}
	if limits.MaxParameters <= 0 {
		limits.MaxParameters = DefaultMaxParameters
	}
	byParameters := limits.MaxParameters / parametersPerRow
	if byParameters < 1 {
		return 1
	}
	if byParameters < limits.MaxRows {
		return byParameters
	}
	return limits.MaxRows
}

// ForEachBatch calls callback for contiguous row ranges [start, end).
func ForEachBatch(
	rowCount int,
	parametersPerRow int,
	limits BatchLimits,
	callback func(start, end int) error,
) error {
	if rowCount <= 0 {
		return nil
	}
	if callback == nil {
		return ErrBatchCallbackRequired
	}
	batchSize := BatchSize(parametersPerRow, limits)
	for start := 0; start < rowCount; start += batchSize {
		end := start + batchSize
		if end > rowCount {
			end = rowCount
		}
		if err := callback(start, end); err != nil {
			return err
		}
	}
	return nil
}
