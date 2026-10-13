package gateway

import (
	"testing"

	"github.com/rtcdance/streamgate/pkg/core/config"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

// TestAcceptance_IsolatedTranscoderWorker pins the role wiring of
// transcoding.enabled (TRANSITION_PLAN.md SG-12 step 5): the durable profile
// keeps untrusted media parsing OUT of this process. With the flag off the
// service is still constructed and submittable, but the embedded worker must
// NOT run — tasks are executed by the external transcoder worker, and this
// process only publishes its outbox intents to the broker.
func TestAcceptance_IsolatedTranscoderWorker(t *testing.T) {
	t.Run("disabled flag keeps embedded worker off", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Transcoding.Enabled = false
		cfg.NATS.URL = "nats://127.0.0.1:1"
		// No Postgres / MinIO / NATS in this unit test: the queue degrades to
		// the in-memory fallback, which is enough to build the service. The
		// role decision does not depend on any of them being reachable.
		svc := provideTranscodingService(cfg, zap.NewNop(), nil, nil, &AppResources{})
		assert.NotNil(t, svc, "submitter-only process must still build a usable service")
		assert.False(t, svc.WorkerRunning(),
			"transcoding.enabled=false must not start the embedded worker")
		svc.StopWorker()
	})

	t.Run("enabled flag starts the embedded worker", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Transcoding.Enabled = true
		cfg.NATS.URL = "nats://127.0.0.1:1"
		svc := provideTranscodingService(cfg, zap.NewNop(), nil, nil, &AppResources{})
		defer svc.StopWorker()
		assert.True(t, svc.WorkerRunning(),
			"transcoding.enabled=true must start the embedded worker (dev/demo profile)")
	})
}
