package worker

import (
	"context"
)

// DefaultExecutor is a default implementation of JobExecutor
type DefaultExecutor struct{}

// NewDefaultExecutor creates a new default executor
func NewDefaultExecutor() *DefaultExecutor {
	return &DefaultExecutor{}
}

// Execute executes a job
func (de *DefaultExecutor) Execute(ctx context.Context, job *Job) (interface{}, error) {
	return nil, nil
}

// CanExecute checks if this executor can execute a job type
func (de *DefaultExecutor) CanExecute(jobType string) bool {
	return true
}
