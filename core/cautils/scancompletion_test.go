package cautils

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type producerFailure struct{}

func (*producerFailure) Error() string { return "producer stopped" }

func TestScanStagePreservesOriginAndErrorIdentity(t *testing.T) {
	producer := &producerFailure{}
	primary := AtScanStage(StageCollection, errors.Join(producer, context.DeadlineExceeded))
	outer := AtScanStage(StageEvaluation, primary)
	require.Same(t, primary, outer)
	require.Equal(t, StageCollection, FailedScanStage(outer, StageInitialization))
	require.ErrorIs(t, outer, context.DeadlineExceeded)
	var typed *producerFailure
	require.ErrorAs(t, outer, &typed)
	require.Same(t, producer, typed)
	finalErr := AtScanStage(StageFinalization, errors.New("finalizer failed"))
	require.Equal(t, StageCollection, FailedScanStage(errors.Join(primary, finalErr), StageInitialization))
	require.Equal(t, StageFinalization, FailedScanStage(finalErr, StageInitialization))
	require.Nil(t, AtScanStage(StageEvaluation, nil))
}
