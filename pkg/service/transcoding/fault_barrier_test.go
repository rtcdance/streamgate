package transcoding

import (
	"context"
	"testing"
	"time"

	"github.com/rtcdance/streamgate/pkg/acceptance/barrier"
	"github.com/rtcdance/streamgate/pkg/core/config"
	"github.com/rtcdance/streamgate/pkg/models"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// durableBarrier builds the rendezvous the durable acceptance profile installs.
func durableBarrier(t *testing.T) *barrier.Barrier {
	t.Helper()
	b, err := barrier.Load(&config.Config{
		Profile: config.ProfileDurable,
		Acceptance: config.AcceptanceConfig{
			FaultBarrier:        true,
			FaultBarrierDir:     t.TempDir(),
			FaultBarrierTimeout: 500 * time.Millisecond,
		},
	}, zap.NewNop())
	require.NoError(t, err)
	require.NotNil(t, b)
	return b
}

// TestFaultBarrier_StopsTheWorkerAtTheClaim pins the worker wiring: with the
// claim point armed, handleDelivery announces worker.claimed and stays there
// until the harness releases it, so a kill at that instant is a real "claimed
// but not yet executed" crash rather than a guessed sleep. Every other point is
// left unarmed, which is how a harness pays only for the fault it injects.
func TestFaultBarrier_StopsTheWorkerAtTheClaim(t *testing.T) {
	b := durableBarrier(t)
	queue := newRecordingQueue()
	svc, db := newDurabilityFixture(t, queue, &mockVideoTranscoder{progressCalls: []float64{100}})
	svc.SetFaultBarrier(b)
	require.NoError(t, b.Arm("worker.claimed"))

	taskID, err := svc.Transcode(context.Background(), "content-barrier", "720p", "/tmp/in.mp4", 5, "0xOwner")
	require.NoError(t, err)
	require.NotNil(t, db.task(taskID))

	done := make(chan struct{})
	go func() {
		defer close(done)
		deliverTo(svc, queue, &models.TranscodingTask{ID: taskID, ContentID: "content-barrier", Profile: "720p"})
	}()

	require.Eventually(t, func() bool { return b.Point("worker.claimed").Reached() },
		2*time.Second, 5*time.Millisecond,
		"the worker must announce the claim before it executes anything")
	select {
	case <-done:
		t.Fatal("the worker ran past worker.claimed without a release")
	case <-time.After(50 * time.Millisecond):
	}

	require.NoError(t, b.Point("worker.claimed").Release())
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the worker did not continue after the release")
	}
}

// TestFaultBarrier_ArmedPointCannotBeOverwrittenByAnOldWorker pins the wiring
// the matrix relies on for "an old worker must not overwrite a new result":
// worker.after_publish sits AFTER the completion transaction commits, so the
// generation is already durable when the point is announced.
func TestFaultBarrier_ArmedPointCannotBeOverwrittenByAnOldWorker(t *testing.T) {
	b := durableBarrier(t)
	queue := newRecordingQueue()
	svc, db := newDurabilityFixture(t, queue, &mockVideoTranscoder{progressCalls: []float64{100}})
	svc.SetFaultBarrier(b)
	require.NoError(t, b.Arm("worker.after_publish"))

	taskID, err := svc.Transcode(context.Background(), "content-published", "720p", "/tmp/in.mp4", 5, "0xOwner")
	require.NoError(t, err)
	require.NotNil(t, db.task(taskID))

	done := make(chan struct{})
	go func() {
		defer close(done)
		deliverTo(svc, queue, &models.TranscodingTask{ID: taskID, ContentID: "content-published", Profile: "720p"})
	}()

	require.Eventually(t, func() bool { return b.Point("worker.after_publish").Reached() },
		5*time.Second, 5*time.Millisecond,
		"the publish point must be announced after the completion transaction commits")

	// The task row is already 'completed' and the generation is already the
	// published one while the worker is still parked at the point: a restart from
	// here cannot publish a second generation.
	row := db.task(taskID)
	require.Equal(t, "completed", row.status, "the completion transaction commits BEFORE the point is announced")

	require.NoError(t, b.Point("worker.after_publish").Release())
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the worker did not continue after the release")
	}
}

// TestFaultBarrier_DisabledIsANoOp pins the other half of the contract: a
// service with no barrier installed runs the same delivery to completion without
// ever creating a marker — the production path costs nothing.
func TestFaultBarrier_DisabledIsANoOp(t *testing.T) {
	queue := newRecordingQueue()
	svc, db := newDurabilityFixture(t, queue, &mockVideoTranscoder{progressCalls: []float64{100}})

	taskID, err := svc.Transcode(context.Background(), "content-nobarrier", "720p", "/tmp/in.mp4", 5, "0xOwner")
	require.NoError(t, err)

	done := make(chan struct{})
	go func() {
		defer close(done)
		deliverTo(svc, queue, &models.TranscodingTask{ID: taskID, ContentID: "content-nobarrier", Profile: "720p"})
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the worker did not finish without a barrier")
	}
	require.Equal(t, "completed", db.task(taskID).status)
}
