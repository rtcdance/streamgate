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

// TestFaultBarrier_StopsTheWorkerAtTheClaim pins the worker wiring: with a
// barrier installed handleDelivery announces worker.claimed and stays there
// until the harness releases it, so a kill at that instant is a real "claimed
// but not yet executed" crash rather than a guessed sleep. Points the scenario
// does not inject at are pre-released, which is how a harness keeps the
// uninteresting parts of the path free of wall clock.
func TestFaultBarrier_StopsTheWorkerAtTheClaim(t *testing.T) {
	b := durableBarrier(t)
	queue := newRecordingQueue()
	svc, db := newDurabilityFixture(t, queue, &mockVideoTranscoder{progressCalls: []float64{100}})
	svc.SetFaultBarrier(b)

	taskID, err := svc.Transcode(context.Background(), "content-barrier", "720p", "/tmp/in.mp4", 5, "0xOwner")
	require.NoError(t, err)
	require.NotNil(t, db.task(taskID))

	for _, name := range []string{"worker.partial_output", "worker.after_publish", "worker.before_ack"} {
		require.NoError(t, b.Point(name).Release())
	}

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
