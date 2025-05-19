package langfuse

import (
	"errors"
	"fmt"

	"github.com/busthorne/cablectl/langfuse/api"
)

var ErrBatchFailed = errors.New("langfuse: batch ingestion failed")

type BatchError struct {
	Errors []api.IngestionError `json:"errors"`
}

func (e *BatchError) Error() string {
	return fmt.Sprintf("langfuse: %d events failed to ingest", len(e.Errors))
}

// Unwrap allows the error to be checked with errors.Is without type assertion
func (e *BatchError) Unwrap() error {
	return ErrBatchFailed
}
