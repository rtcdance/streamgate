package gateway

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/rtcdance/streamgate/pkg/service"
)

type mockCloser struct {
	closed bool
	err    error
}

func (m *mockCloser) Close() error {
	m.closed = true
	return m.err
}

type mockRateLimiter struct {
	stopped bool
}

func (m *mockRateLimiter) Allow(_ context.Context, _ string) bool { return true }
func (m *mockRateLimiter) Stop()                                  { m.stopped = true }

func TestAppResources_Close_NilFields(t *testing.T) {
	r := &AppResources{}
	err := r.Close()
	assert.NoError(t, err)
}

func TestAppResources_Close_DBError(t *testing.T) {
	db, _ := sql.Open("postgres", "placeholder")
	r := &AppResources{DB: db}
	err := r.Close()
	assert.NoError(t, err)
}

func TestAppResources_Close_ChallengeStoreError(t *testing.T) {
	cs := &mockCloser{err: errors.New("close fail")}
	r := &AppResources{ChallengeStore: cs}
	err := r.Close()
	assert.Error(t, err)
	assert.True(t, cs.closed)
}

func TestAppResources_Close_ObjStorageError(t *testing.T) {
	os := &mockCloser{err: errors.New("obj fail")}
	r := &AppResources{ObjStorage: os}
	err := r.Close()
	assert.Error(t, err)
	assert.True(t, os.closed)
}

func TestAppResources_Close_TokenBlacklistError(t *testing.T) {
	tb := &mockCloser{err: errors.New("tb fail")}
	r := &AppResources{TokenBlacklist: tb}
	err := r.Close()
	assert.Error(t, err)
	assert.True(t, tb.closed)
}

func TestAppResources_Close_RateLimiter(t *testing.T) {
	rl := &mockRateLimiter{}
	arl := &mockRateLimiter{}
	r := &AppResources{RateLimiter: rl, AuthRateLimiter: arl}
	err := r.Close()
	assert.NoError(t, err)
	assert.True(t, rl.stopped)
	assert.True(t, arl.stopped)
}

func TestAppResources_Close_NFTCache(t *testing.T) {
	cache := NewNFTAccessCache()
	r := &AppResources{NFTCache: cache}
	err := r.Close()
	assert.NoError(t, err)
}

func TestAppResources_Close_OTelShutdown(t *testing.T) {
	r := &AppResources{
		OTelShutdown: func(ctx context.Context) error {
			return nil
		},
	}
	err := r.Close()
	assert.NoError(t, err)
}

func TestAppResources_Close_OTelShutdownError(t *testing.T) {
	r := &AppResources{
		OTelShutdown: func(ctx context.Context) error {
			return errors.New("otel fail")
		},
	}
	err := r.Close()
	assert.Error(t, err)
}

func TestWithAuthService(t *testing.T) {
	cfg := &RouterConfig{}
	opt := WithAuthService(&service.AuthService{})
	opt(cfg)
	assert.NotNil(t, cfg.AuthService)
}

func TestWithWeb3Service(t *testing.T) {
	cfg := &RouterConfig{}
	opt := WithWeb3Service(&service.Web3Service{})
	opt(cfg)
	assert.NotNil(t, cfg.Web3Service)
}

func TestWithSegmentStorage(t *testing.T) {
	cfg := &RouterConfig{}
	opt := WithSegmentStorage(nil)
	opt(cfg)
	assert.Nil(t, cfg.SegmentStorage)
}

func TestWithChallengeStore(t *testing.T) {
	cfg := &RouterConfig{}
	opt := WithChallengeStore(nil)
	opt(cfg)
	assert.Nil(t, cfg.ChallengeStore)
}

func TestWithNFTVerifier(t *testing.T) {
	cfg := &RouterConfig{}
	opt := WithNFTVerifier(nil)
	opt(cfg)
	assert.Nil(t, cfg.NFTVerifier)
}

func TestWithContentService(t *testing.T) {
	cfg := &RouterConfig{}
	opt := WithContentService(&service.ContentService{})
	opt(cfg)
	assert.NotNil(t, cfg.ContentService)
}

func TestWithUploadService(t *testing.T) {
	cfg := &RouterConfig{}
	opt := WithUploadService(&service.UploadService{})
	opt(cfg)
	assert.NotNil(t, cfg.UploadService)
}

func TestAppResources_Close_MultipleErrors(t *testing.T) {
	r := &AppResources{
		ChallengeStore: &mockCloser{err: errors.New("err1")},
		ObjStorage:     &mockCloser{err: errors.New("err2")},
	}
	err := r.Close()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "err1")
	assert.Contains(t, err.Error(), "err2")
}

func TestAppResources_Close_TranscodingSvc(t *testing.T) {
	r := &AppResources{
		TranscodingSvc: &service.TranscodingService{},
	}
	err := r.Close()
	assert.NoError(t, err)
}

func TestAppResources_Close_UploadService(t *testing.T) {
	r := &AppResources{
		UploadService: &service.UploadService{},
	}
	err := r.Close()
	assert.NoError(t, err)
}

func TestAppResources_Close_NATSQueue(t *testing.T) {
	nq := &mockCloser{}
	r := &AppResources{NATSQueue: nq}
	err := r.Close()
	assert.NoError(t, err)
	assert.True(t, nq.closed)
}
