package servicev1

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

func TestEnum_ServingStatus_Methods(t *testing.T) {
	tests := []struct {
		name  string
		value HealthCheckResponse_ServingStatus
		str   string
		num   int32
	}{
		{"UNKNOWN", HealthCheckResponse_UNKNOWN, "UNKNOWN", 0},
		{"SERVING", HealthCheckResponse_SERVING, "SERVING", 1},
		{"NOT_SERVING", HealthCheckResponse_NOT_SERVING, "NOT_SERVING", 2},
		{"SERVICE_UNKNOWN", HealthCheckResponse_SERVICE_UNKNOWN, "SERVICE_UNKNOWN", 3},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.str, tc.value.String())
			p := tc.value.Enum()
			assert.Equal(t, tc.value, *p)
			assert.Equal(t, tc.num, int32(tc.value.Number()))
			assert.NotNil(t, tc.value.Descriptor())
			assert.NotNil(t, tc.value.Type())
		})
	}
}

func TestMessage_Reset(t *testing.T) {
	(&HealthCheckRequest{Service: "test"}).Reset()
	(&HealthCheckResponse{Status: HealthCheckResponse_SERVING}).Reset()
	(&UploadFileRequest{FileName: "f.mp4", FileSize: 100, ContentType: "video/mp4"}).Reset()
	(&UploadFileResponse{UploadId: "u1", FileId: "f1"}).Reset()
	(&GetUploadStatusRequest{UploadId: "u1"}).Reset()
	(&UploadStatus{UploadId: "u1", Status: "in_progress", Progress: 50, UploadedBytes: 512}).Reset()
	(&CompleteUploadRequest{UploadId: "u1"}).Reset()
	(&CompleteUploadResponse{FileId: "f1", FileSize: 1024}).Reset()
	(&GetPlaylistRequest{ContentId: "c1"}).Reset()
	(&PlaylistResponse{Playlist: "#EXTM3U", ContentType: "application/vnd.apple.mpegurl"}).Reset()
	(&GetManifestRequest{ContentId: "c1"}).Reset()
	(&ManifestResponse{Manifest: "<MPD>", ContentType: "application/dash+xml"}).Reset()
	(&GetSegmentRequest{ContentId: "c1", SegmentId: "s1"}).Reset()
	(&SegmentResponse{Data: []byte("seg"), ContentType: "video/mp2t"}).Reset()
	(&GetMetadataRequest{ContentId: "c1"}).Reset()
	(&CreateMetadataRequest{Title: "T", Description: "D", Duration: 120, Format: "mp4"}).Reset()
	(&UpdateMetadataRequest{ContentId: "c1", Title: "T", Description: "D"}).Reset()
	(&DeleteMetadataRequest{ContentId: "c1"}).Reset()
	(&Metadata{ContentId: "c1", Title: "T", Description: "D", Duration: 120, FileSize: 1024, Format: "mp4"}).Reset()
	(&SearchMetadataRequest{Query: "q", Limit: 10, Offset: 0}).Reset()
	(&SearchMetadataResponse{Results: []*Metadata{{ContentId: "c1"}}, Total: 1}).Reset()
	(&VerifySignatureRequest{Address: "0x1", Message: "m", Signature: "s"}).Reset()
	(&VerifySignatureResponse{Valid: true, Error: "e"}).Reset()
	(&VerifyNFTRequest{Address: "0x1", ContractAddress: "0xC", TokenId: "1"}).Reset()
	(&VerifyNFTResponse{Valid: true, Error: "e"}).Reset()
	(&VerifyTokenRequest{Token: "tok"}).Reset()
	(&VerifyTokenResponse{Valid: true, Error: "e"}).Reset()
	(&GetChallengeRequest{Address: "0x1"}).Reset()
	(&GetChallengeResponse{Challenge: "ch", ExpiresAt: 999}).Reset()
	(&GetCacheRequest{Key: "k"}).Reset()
	(&GetCacheResponse{Value: []byte("v"), Found: true}).Reset()
	(&SetCacheRequest{Key: "k", Value: []byte("v"), Ttl: 300}).Reset()
	(&DeleteCacheRequest{Key: "k"}).Reset()
	(&SubmitJobRequest{InputFile: "/in", OutputFile: "/out", Format: "hls", Bitrate: "2500k", Resolution: "720p"}).Reset()
	(&SubmitJobResponse{JobId: "j1"}).Reset()
	(&GetJobStatusRequest{JobId: "j1"}).Reset()
	(&JobStatus{JobId: "j1", Status: "running", Progress: 50, Error: "e"}).Reset()
	(&CancelJobRequest{JobId: "j1"}).Reset()
	(&SubmitWorkerJobRequest{JobType: "t", Payload: map[string]string{"k": "v"}}).Reset()
	(&SubmitWorkerJobResponse{JobId: "j1"}).Reset()
	(&GetWorkerJobStatusRequest{JobId: "j1"}).Reset()
	(&WorkerJobStatus{JobId: "j1", Status: "done", Error: "e"}).Reset()
	(&CancelWorkerJobRequest{JobId: "j1"}).Reset()
	(&ScheduleJobRequest{JobType: "t", Schedule: "0 * * * *"}).Reset()
	(&ScheduleJobResponse{ScheduledJobId: "s1"}).Reset()
	(&HealthStatus{Status: "ok", Services: map[string]string{"a": "up"}}).Reset()
	(&SystemMetrics{CpuUsage: 45.5, MemoryUsage: 60.2, RequestCount: 1000, ErrorCount: 5, AvgLatency: 120.5}).Reset()
	(&AlertsResponse{Alerts: []*Alert{{Id: "a1"}}}).Reset()
	(&Alert{Id: "a1", Level: "warn", Message: "m"}).Reset()
}

func TestMessage_String(t *testing.T) {
	assert.NotEmpty(t, (&HealthCheckRequest{Service: "auth"}).String())
	assert.NotEmpty(t, (&HealthCheckResponse{Status: HealthCheckResponse_SERVING}).String())
	assert.NotEmpty(t, (&UploadFileRequest{FileName: "f.mp4"}).String())
	assert.NotEmpty(t, (&UploadFileResponse{UploadId: "u1"}).String())
	assert.NotEmpty(t, (&GetUploadStatusRequest{UploadId: "u1"}).String())
	assert.NotEmpty(t, (&UploadStatus{UploadId: "u1"}).String())
	assert.NotEmpty(t, (&CompleteUploadRequest{UploadId: "u1"}).String())
	assert.NotEmpty(t, (&CompleteUploadResponse{FileId: "f1"}).String())
	assert.NotEmpty(t, (&GetPlaylistRequest{ContentId: "c1"}).String())
	assert.NotEmpty(t, (&PlaylistResponse{Playlist: "#EXTM3U"}).String())
	assert.NotEmpty(t, (&GetManifestRequest{ContentId: "c1"}).String())
	assert.NotEmpty(t, (&ManifestResponse{Manifest: "<MPD>"}).String())
	assert.NotEmpty(t, (&GetSegmentRequest{ContentId: "c1"}).String())
	assert.NotEmpty(t, (&SegmentResponse{Data: []byte("seg")}).String())
	assert.NotEmpty(t, (&GetMetadataRequest{ContentId: "c1"}).String())
	assert.NotEmpty(t, (&CreateMetadataRequest{Title: "T"}).String())
	assert.NotEmpty(t, (&UpdateMetadataRequest{ContentId: "c1"}).String())
	assert.NotEmpty(t, (&DeleteMetadataRequest{ContentId: "c1"}).String())
	assert.NotEmpty(t, (&Metadata{ContentId: "c1"}).String())
	assert.NotEmpty(t, (&SearchMetadataRequest{Query: "q"}).String())
	assert.NotEmpty(t, (&SearchMetadataResponse{Total: 1}).String())
	assert.NotEmpty(t, (&VerifySignatureRequest{Address: "0x1"}).String())
	assert.NotEmpty(t, (&VerifySignatureResponse{Valid: true}).String())
	assert.NotEmpty(t, (&VerifyNFTRequest{Address: "0x1"}).String())
	assert.NotEmpty(t, (&VerifyNFTResponse{Valid: true}).String())
	assert.NotEmpty(t, (&VerifyTokenRequest{Token: "tok"}).String())
	assert.NotEmpty(t, (&VerifyTokenResponse{Valid: true}).String())
	assert.NotEmpty(t, (&GetChallengeRequest{Address: "0x1"}).String())
	assert.NotEmpty(t, (&GetChallengeResponse{Challenge: "ch"}).String())
	assert.NotEmpty(t, (&GetCacheRequest{Key: "k"}).String())
	assert.NotEmpty(t, (&GetCacheResponse{Found: true}).String())
	assert.NotEmpty(t, (&SetCacheRequest{Key: "k"}).String())
	assert.NotEmpty(t, (&DeleteCacheRequest{Key: "k"}).String())
	assert.NotEmpty(t, (&SubmitJobRequest{InputFile: "/in"}).String())
	assert.NotEmpty(t, (&SubmitJobResponse{JobId: "j1"}).String())
	assert.NotEmpty(t, (&GetJobStatusRequest{JobId: "j1"}).String())
	assert.NotEmpty(t, (&JobStatus{JobId: "j1"}).String())
	assert.NotEmpty(t, (&CancelJobRequest{JobId: "j1"}).String())
	assert.NotEmpty(t, (&SubmitWorkerJobRequest{JobType: "t"}).String())
	assert.NotEmpty(t, (&SubmitWorkerJobResponse{JobId: "j1"}).String())
	assert.NotEmpty(t, (&GetWorkerJobStatusRequest{JobId: "j1"}).String())
	assert.NotEmpty(t, (&WorkerJobStatus{JobId: "j1"}).String())
	assert.NotEmpty(t, (&CancelWorkerJobRequest{JobId: "j1"}).String())
	assert.NotEmpty(t, (&ScheduleJobRequest{JobType: "t"}).String())
	assert.NotEmpty(t, (&ScheduleJobResponse{ScheduledJobId: "s1"}).String())
	assert.NotEmpty(t, (&HealthStatus{Status: "ok"}).String())
	assert.NotEmpty(t, (&SystemMetrics{CpuUsage: 50.0}).String())
	assert.NotEmpty(t, (&AlertsResponse{Alerts: []*Alert{{Id: "a1"}}}).String())
	assert.NotEmpty(t, (&Alert{Id: "a1"}).String())
}

func TestMessage_ProtoMessage(t *testing.T) {
	(&HealthCheckRequest{}).ProtoMessage()
	(&HealthCheckResponse{}).ProtoMessage()
	(&UploadFileRequest{}).ProtoMessage()
	(&UploadFileResponse{}).ProtoMessage()
	(&GetUploadStatusRequest{}).ProtoMessage()
	(&UploadStatus{}).ProtoMessage()
	(&CompleteUploadRequest{}).ProtoMessage()
	(&CompleteUploadResponse{}).ProtoMessage()
	(&GetPlaylistRequest{}).ProtoMessage()
	(&PlaylistResponse{}).ProtoMessage()
	(&GetManifestRequest{}).ProtoMessage()
	(&ManifestResponse{}).ProtoMessage()
	(&GetSegmentRequest{}).ProtoMessage()
	(&SegmentResponse{}).ProtoMessage()
	(&GetMetadataRequest{}).ProtoMessage()
	(&CreateMetadataRequest{}).ProtoMessage()
	(&UpdateMetadataRequest{}).ProtoMessage()
	(&DeleteMetadataRequest{}).ProtoMessage()
	(&Metadata{}).ProtoMessage()
	(&SearchMetadataRequest{}).ProtoMessage()
	(&SearchMetadataResponse{}).ProtoMessage()
	(&VerifySignatureRequest{}).ProtoMessage()
	(&VerifySignatureResponse{}).ProtoMessage()
	(&VerifyNFTRequest{}).ProtoMessage()
	(&VerifyNFTResponse{}).ProtoMessage()
	(&VerifyTokenRequest{}).ProtoMessage()
	(&VerifyTokenResponse{}).ProtoMessage()
	(&GetChallengeRequest{}).ProtoMessage()
	(&GetChallengeResponse{}).ProtoMessage()
	(&GetCacheRequest{}).ProtoMessage()
	(&GetCacheResponse{}).ProtoMessage()
	(&SetCacheRequest{}).ProtoMessage()
	(&DeleteCacheRequest{}).ProtoMessage()
	(&SubmitJobRequest{}).ProtoMessage()
	(&SubmitJobResponse{}).ProtoMessage()
	(&GetJobStatusRequest{}).ProtoMessage()
	(&JobStatus{}).ProtoMessage()
	(&CancelJobRequest{}).ProtoMessage()
	(&SubmitWorkerJobRequest{}).ProtoMessage()
	(&SubmitWorkerJobResponse{}).ProtoMessage()
	(&GetWorkerJobStatusRequest{}).ProtoMessage()
	(&WorkerJobStatus{}).ProtoMessage()
	(&CancelWorkerJobRequest{}).ProtoMessage()
	(&ScheduleJobRequest{}).ProtoMessage()
	(&ScheduleJobResponse{}).ProtoMessage()
	(&HealthStatus{}).ProtoMessage()
	(&SystemMetrics{}).ProtoMessage()
	(&AlertsResponse{}).ProtoMessage()
	(&Alert{}).ProtoMessage()
}

func TestMessage_ProtoReflect(t *testing.T) {
	assert.NotNil(t, (&HealthCheckRequest{Service: "auth"}).ProtoReflect())
	assert.NotNil(t, (&HealthCheckResponse{Status: HealthCheckResponse_SERVING}).ProtoReflect())
	assert.NotNil(t, (&UploadFileRequest{FileName: "f.mp4"}).ProtoReflect())
	assert.NotNil(t, (&UploadFileResponse{UploadId: "u1"}).ProtoReflect())
	assert.NotNil(t, (&GetUploadStatusRequest{UploadId: "u1"}).ProtoReflect())
	assert.NotNil(t, (&UploadStatus{UploadId: "u1"}).ProtoReflect())
	assert.NotNil(t, (&CompleteUploadRequest{UploadId: "u1"}).ProtoReflect())
	assert.NotNil(t, (&CompleteUploadResponse{FileId: "f1"}).ProtoReflect())
	assert.NotNil(t, (&GetPlaylistRequest{ContentId: "c1"}).ProtoReflect())
	assert.NotNil(t, (&PlaylistResponse{Playlist: "#EXTM3U"}).ProtoReflect())
	assert.NotNil(t, (&GetManifestRequest{ContentId: "c1"}).ProtoReflect())
	assert.NotNil(t, (&ManifestResponse{Manifest: "<MPD>"}).ProtoReflect())
	assert.NotNil(t, (&GetSegmentRequest{ContentId: "c1"}).ProtoReflect())
	assert.NotNil(t, (&SegmentResponse{Data: []byte("seg")}).ProtoReflect())
	assert.NotNil(t, (&GetMetadataRequest{ContentId: "c1"}).ProtoReflect())
	assert.NotNil(t, (&CreateMetadataRequest{Title: "T"}).ProtoReflect())
	assert.NotNil(t, (&UpdateMetadataRequest{ContentId: "c1"}).ProtoReflect())
	assert.NotNil(t, (&DeleteMetadataRequest{ContentId: "c1"}).ProtoReflect())
	assert.NotNil(t, (&Metadata{ContentId: "c1"}).ProtoReflect())
	assert.NotNil(t, (&SearchMetadataRequest{Query: "q"}).ProtoReflect())
	assert.NotNil(t, (&SearchMetadataResponse{Total: 1}).ProtoReflect())
	assert.NotNil(t, (&VerifySignatureRequest{Address: "0x1"}).ProtoReflect())
	assert.NotNil(t, (&VerifySignatureResponse{Valid: true}).ProtoReflect())
	assert.NotNil(t, (&VerifyNFTRequest{Address: "0x1"}).ProtoReflect())
	assert.NotNil(t, (&VerifyNFTResponse{Valid: true}).ProtoReflect())
	assert.NotNil(t, (&VerifyTokenRequest{Token: "tok"}).ProtoReflect())
	assert.NotNil(t, (&VerifyTokenResponse{Valid: true}).ProtoReflect())
	assert.NotNil(t, (&GetChallengeRequest{Address: "0x1"}).ProtoReflect())
	assert.NotNil(t, (&GetChallengeResponse{Challenge: "ch"}).ProtoReflect())
	assert.NotNil(t, (&GetCacheRequest{Key: "k"}).ProtoReflect())
	assert.NotNil(t, (&GetCacheResponse{Found: true}).ProtoReflect())
	assert.NotNil(t, (&SetCacheRequest{Key: "k"}).ProtoReflect())
	assert.NotNil(t, (&DeleteCacheRequest{Key: "k"}).ProtoReflect())
	assert.NotNil(t, (&SubmitJobRequest{InputFile: "/in"}).ProtoReflect())
	assert.NotNil(t, (&SubmitJobResponse{JobId: "j1"}).ProtoReflect())
	assert.NotNil(t, (&GetJobStatusRequest{JobId: "j1"}).ProtoReflect())
	assert.NotNil(t, (&JobStatus{JobId: "j1"}).ProtoReflect())
	assert.NotNil(t, (&CancelJobRequest{JobId: "j1"}).ProtoReflect())
	assert.NotNil(t, (&SubmitWorkerJobRequest{JobType: "t"}).ProtoReflect())
	assert.NotNil(t, (&SubmitWorkerJobResponse{JobId: "j1"}).ProtoReflect())
	assert.NotNil(t, (&GetWorkerJobStatusRequest{JobId: "j1"}).ProtoReflect())
	assert.NotNil(t, (&WorkerJobStatus{JobId: "j1"}).ProtoReflect())
	assert.NotNil(t, (&CancelWorkerJobRequest{JobId: "j1"}).ProtoReflect())
	assert.NotNil(t, (&ScheduleJobRequest{JobType: "t"}).ProtoReflect())
	assert.NotNil(t, (&ScheduleJobResponse{ScheduledJobId: "s1"}).ProtoReflect())
	assert.NotNil(t, (&HealthStatus{Status: "ok"}).ProtoReflect())
	assert.NotNil(t, (&SystemMetrics{CpuUsage: 50.0}).ProtoReflect())
	assert.NotNil(t, (&AlertsResponse{}).ProtoReflect())
	assert.NotNil(t, (&Alert{Id: "a1"}).ProtoReflect())
}

func TestMessage_Descriptor(t *testing.T) {
	rawDesc, _ := (&HealthCheckRequest{}).Descriptor()
	assert.NotNil(t, rawDesc)
	rawDesc, _ = (&HealthCheckResponse{}).Descriptor()
	assert.NotNil(t, rawDesc)
	rawDesc, _ = (&UploadFileRequest{}).Descriptor()
	assert.NotNil(t, rawDesc)
	rawDesc, _ = (&UploadFileResponse{}).Descriptor()
	assert.NotNil(t, rawDesc)
	rawDesc, _ = (&GetUploadStatusRequest{}).Descriptor()
	assert.NotNil(t, rawDesc)
	rawDesc, _ = (&UploadStatus{}).Descriptor()
	assert.NotNil(t, rawDesc)
	rawDesc, _ = (&CompleteUploadRequest{}).Descriptor()
	assert.NotNil(t, rawDesc)
	rawDesc, _ = (&CompleteUploadResponse{}).Descriptor()
	assert.NotNil(t, rawDesc)
	rawDesc, _ = (&GetPlaylistRequest{}).Descriptor()
	assert.NotNil(t, rawDesc)
	rawDesc, _ = (&PlaylistResponse{}).Descriptor()
	assert.NotNil(t, rawDesc)
	rawDesc, _ = (&GetManifestRequest{}).Descriptor()
	assert.NotNil(t, rawDesc)
	rawDesc, _ = (&ManifestResponse{}).Descriptor()
	assert.NotNil(t, rawDesc)
	rawDesc, _ = (&GetSegmentRequest{}).Descriptor()
	assert.NotNil(t, rawDesc)
	rawDesc, _ = (&SegmentResponse{}).Descriptor()
	assert.NotNil(t, rawDesc)
	rawDesc, _ = (&GetMetadataRequest{}).Descriptor()
	assert.NotNil(t, rawDesc)
	rawDesc, _ = (&CreateMetadataRequest{}).Descriptor()
	assert.NotNil(t, rawDesc)
	rawDesc, _ = (&UpdateMetadataRequest{}).Descriptor()
	assert.NotNil(t, rawDesc)
	rawDesc, _ = (&DeleteMetadataRequest{}).Descriptor()
	assert.NotNil(t, rawDesc)
	rawDesc, _ = (&Metadata{}).Descriptor()
	assert.NotNil(t, rawDesc)
	rawDesc, _ = (&SearchMetadataRequest{}).Descriptor()
	assert.NotNil(t, rawDesc)
	rawDesc, _ = (&SearchMetadataResponse{}).Descriptor()
	assert.NotNil(t, rawDesc)
	rawDesc, _ = (&VerifySignatureRequest{}).Descriptor()
	assert.NotNil(t, rawDesc)
	rawDesc, _ = (&VerifySignatureResponse{}).Descriptor()
	assert.NotNil(t, rawDesc)
	rawDesc, _ = (&VerifyNFTRequest{}).Descriptor()
	assert.NotNil(t, rawDesc)
	rawDesc, _ = (&VerifyNFTResponse{}).Descriptor()
	assert.NotNil(t, rawDesc)
	rawDesc, _ = (&VerifyTokenRequest{}).Descriptor()
	assert.NotNil(t, rawDesc)
	rawDesc, _ = (&VerifyTokenResponse{}).Descriptor()
	assert.NotNil(t, rawDesc)
	rawDesc, _ = (&GetChallengeRequest{}).Descriptor()
	assert.NotNil(t, rawDesc)
	rawDesc, _ = (&GetChallengeResponse{}).Descriptor()
	assert.NotNil(t, rawDesc)
	rawDesc, _ = (&GetCacheRequest{}).Descriptor()
	assert.NotNil(t, rawDesc)
	rawDesc, _ = (&GetCacheResponse{}).Descriptor()
	assert.NotNil(t, rawDesc)
	rawDesc, _ = (&SetCacheRequest{}).Descriptor()
	assert.NotNil(t, rawDesc)
	rawDesc, _ = (&DeleteCacheRequest{}).Descriptor()
	assert.NotNil(t, rawDesc)
	rawDesc, _ = (&SubmitJobRequest{}).Descriptor()
	assert.NotNil(t, rawDesc)
	rawDesc, _ = (&SubmitJobResponse{}).Descriptor()
	assert.NotNil(t, rawDesc)
	rawDesc, _ = (&GetJobStatusRequest{}).Descriptor()
	assert.NotNil(t, rawDesc)
	rawDesc, _ = (&JobStatus{}).Descriptor()
	assert.NotNil(t, rawDesc)
	rawDesc, _ = (&CancelJobRequest{}).Descriptor()
	assert.NotNil(t, rawDesc)
	rawDesc, _ = (&SubmitWorkerJobRequest{}).Descriptor()
	assert.NotNil(t, rawDesc)
	rawDesc, _ = (&SubmitWorkerJobResponse{}).Descriptor()
	assert.NotNil(t, rawDesc)
	rawDesc, _ = (&GetWorkerJobStatusRequest{}).Descriptor()
	assert.NotNil(t, rawDesc)
	rawDesc, _ = (&WorkerJobStatus{}).Descriptor()
	assert.NotNil(t, rawDesc)
	rawDesc, _ = (&CancelWorkerJobRequest{}).Descriptor()
	assert.NotNil(t, rawDesc)
	rawDesc, _ = (&ScheduleJobRequest{}).Descriptor()
	assert.NotNil(t, rawDesc)
	rawDesc, _ = (&ScheduleJobResponse{}).Descriptor()
	assert.NotNil(t, rawDesc)
	rawDesc, _ = (&HealthStatus{}).Descriptor()
	assert.NotNil(t, rawDesc)
	rawDesc, _ = (&SystemMetrics{}).Descriptor()
	assert.NotNil(t, rawDesc)
	rawDesc, _ = (&AlertsResponse{}).Descriptor()
	assert.NotNil(t, rawDesc)
	rawDesc, _ = (&Alert{}).Descriptor()
	assert.NotNil(t, rawDesc)
}

type mockSvcRegistrar struct {
	services []*grpc.ServiceDesc
}

func (m *mockSvcRegistrar) RegisterService(desc *grpc.ServiceDesc, _ interface{}) {
	m.services = append(m.services, desc)
}

func TestRegisterService_AllServices(t *testing.T) {
	reg := &mockSvcRegistrar{}

	RegisterHealthServiceServer(reg, UnimplementedHealthServiceServer{})
	RegisterSvcUploadServiceServer(reg, UnimplementedSvcUploadServiceServer{})
	RegisterSvcStreamingServiceServer(reg, UnimplementedSvcStreamingServiceServer{})
	RegisterMetadataServiceServer(reg, UnimplementedMetadataServiceServer{})
	RegisterSvcAuthServiceServer(reg, UnimplementedSvcAuthServiceServer{})
	RegisterCacheServiceServer(reg, UnimplementedCacheServiceServer{})
	RegisterTranscoderServiceServer(reg, UnimplementedTranscoderServiceServer{})
	RegisterWorkerServiceServer(reg, UnimplementedWorkerServiceServer{})
	RegisterMonitorServiceServer(reg, UnimplementedMonitorServiceServer{})

	assert.Len(t, reg.services, 9)
	names := make(map[string]bool)
	for _, s := range reg.services {
		names[s.ServiceName] = true
	}
	assert.True(t, names["streamgate.v1.HealthService"])
	assert.True(t, names["streamgate.v1.UploadService"])
	assert.True(t, names["streamgate.v1.StreamingService"])
	assert.True(t, names["streamgate.v1.MetadataService"])
	assert.True(t, names["streamgate.v1.AuthService"])
	assert.True(t, names["streamgate.v1.CacheService"])
	assert.True(t, names["streamgate.v1.TranscoderService"])
	assert.True(t, names["streamgate.v1.WorkerService"])
	assert.True(t, names["streamgate.v1.MonitorService"])
}

type mockCC struct {
	methods []string
}

func (m *mockCC) Invoke(ctx context.Context, method string, _, _ interface{}, _ ...grpc.CallOption) error {
	m.methods = append(m.methods, method)
	return nil
}

func (m *mockCC) NewStream(ctx context.Context, _ *grpc.StreamDesc, method string, _ ...grpc.CallOption) (grpc.ClientStream, error) {
	m.methods = append(m.methods, method)
	return nil, status.Errorf(codes.Unimplemented, "stream not supported in mock")
}

func TestClientConstructors_AllServices(t *testing.T) {
	ctx := context.Background()

	t.Run("HealthService", func(t *testing.T) {
		cc := &mockCC{}
		client := NewHealthServiceClient(cc)
		_, _ = client.Check(ctx, &HealthCheckRequest{})
		assert.Contains(t, cc.methods, "/streamgate.v1.HealthService/Check")
	})

	t.Run("UploadService", func(t *testing.T) {
		cc := &mockCC{}
		client := NewSvcUploadServiceClient(cc)
		_, _ = client.UploadFile(ctx, &UploadFileRequest{})
		_, _ = client.GetUploadStatus(ctx, &GetUploadStatusRequest{})
		_, _ = client.CompleteUpload(ctx, &CompleteUploadRequest{})
		assert.Len(t, cc.methods, 3)
	})

	t.Run("StreamingService", func(t *testing.T) {
		cc := &mockCC{}
		client := NewSvcStreamingServiceClient(cc)
		_, _ = client.GetHLSPlaylist(ctx, &GetPlaylistRequest{})
		_, _ = client.GetDASHManifest(ctx, &GetManifestRequest{})
		_, _ = client.GetSegment(ctx, &GetSegmentRequest{})
		assert.Len(t, cc.methods, 3)
	})

	t.Run("MetadataService", func(t *testing.T) {
		cc := &mockCC{}
		client := NewMetadataServiceClient(cc)
		_, _ = client.GetMetadata(ctx, &GetMetadataRequest{})
		_, _ = client.CreateMetadata(ctx, &CreateMetadataRequest{})
		_, _ = client.UpdateMetadata(ctx, &UpdateMetadataRequest{})
		_, _ = client.DeleteMetadata(ctx, &DeleteMetadataRequest{})
		_, _ = client.SearchMetadata(ctx, &SearchMetadataRequest{})
		assert.Len(t, cc.methods, 5)
	})

	t.Run("AuthService", func(t *testing.T) {
		cc := &mockCC{}
		client := NewSvcAuthServiceClient(cc)
		_, _ = client.VerifySignature(ctx, &VerifySignatureRequest{})
		_, _ = client.VerifyNFT(ctx, &VerifyNFTRequest{})
		_, _ = client.VerifyToken(ctx, &VerifyTokenRequest{})
		_, _ = client.GetChallenge(ctx, &GetChallengeRequest{})
		assert.Len(t, cc.methods, 4)
	})

	t.Run("CacheService", func(t *testing.T) {
		cc := &mockCC{}
		client := NewCacheServiceClient(cc)
		_, _ = client.Get(ctx, &GetCacheRequest{})
		_, _ = client.Set(ctx, &SetCacheRequest{})
		_, _ = client.Delete(ctx, &DeleteCacheRequest{})
		_, _ = client.Clear(ctx, &emptypb.Empty{})
		assert.Len(t, cc.methods, 4)
	})

	t.Run("TranscoderService", func(t *testing.T) {
		cc := &mockCC{}
		client := NewTranscoderServiceClient(cc)
		_, _ = client.SubmitJob(ctx, &SubmitJobRequest{})
		_, _ = client.GetJobStatus(ctx, &GetJobStatusRequest{})
		_, _ = client.CancelJob(ctx, &CancelJobRequest{})
		assert.Len(t, cc.methods, 3)
	})

	t.Run("WorkerService", func(t *testing.T) {
		cc := &mockCC{}
		client := NewWorkerServiceClient(cc)
		_, _ = client.SubmitJob(ctx, &SubmitWorkerJobRequest{})
		_, _ = client.GetJobStatus(ctx, &GetWorkerJobStatusRequest{})
		_, _ = client.CancelJob(ctx, &CancelWorkerJobRequest{})
		_, _ = client.ScheduleJob(ctx, &ScheduleJobRequest{})
		assert.Len(t, cc.methods, 4)
	})

	t.Run("MonitorService", func(t *testing.T) {
		cc := &mockCC{}
		client := NewMonitorServiceClient(cc)
		_, _ = client.GetHealth(ctx, &emptypb.Empty{})
		_, _ = client.GetMetrics(ctx, &emptypb.Empty{})
		_, _ = client.GetAlerts(ctx, &emptypb.Empty{})
		assert.Len(t, cc.methods, 3)
	})
}

func TestHandler_WithInterceptor(t *testing.T) {
	interceptor := func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		return handler(ctx, req)
	}
	noopDec := func(in interface{}) error { return nil }

	tests := []struct {
		name    string
		handler func(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error)
		srv     interface{}
	}{
		{"HealthService_Check", _HealthService_Check_Handler, UnimplementedHealthServiceServer{}},
		{"UploadService_UploadFile", _SvcUploadService_UploadFile_Handler, UnimplementedSvcUploadServiceServer{}},
		{"UploadService_GetUploadStatus", _SvcUploadService_GetUploadStatus_Handler, UnimplementedSvcUploadServiceServer{}},
		{"UploadService_CompleteUpload", _SvcUploadService_CompleteUpload_Handler, UnimplementedSvcUploadServiceServer{}},
		{"StreamingService_GetHLSPlaylist", _SvcStreamingService_GetHLSPlaylist_Handler, UnimplementedSvcStreamingServiceServer{}},
		{"StreamingService_GetDASHManifest", _SvcStreamingService_GetDASHManifest_Handler, UnimplementedSvcStreamingServiceServer{}},
		{"StreamingService_GetSegment", _SvcStreamingService_GetSegment_Handler, UnimplementedSvcStreamingServiceServer{}},
		{"MetadataService_GetMetadata", _MetadataService_GetMetadata_Handler, UnimplementedMetadataServiceServer{}},
		{"MetadataService_CreateMetadata", _MetadataService_CreateMetadata_Handler, UnimplementedMetadataServiceServer{}},
		{"MetadataService_UpdateMetadata", _MetadataService_UpdateMetadata_Handler, UnimplementedMetadataServiceServer{}},
		{"MetadataService_DeleteMetadata", _MetadataService_DeleteMetadata_Handler, UnimplementedMetadataServiceServer{}},
		{"MetadataService_SearchMetadata", _MetadataService_SearchMetadata_Handler, UnimplementedMetadataServiceServer{}},
		{"AuthService_VerifySignature", _SvcAuthService_VerifySignature_Handler, UnimplementedSvcAuthServiceServer{}},
		{"AuthService_VerifyNFT", _SvcAuthService_VerifyNFT_Handler, UnimplementedSvcAuthServiceServer{}},
		{"AuthService_VerifyToken", _SvcAuthService_VerifyToken_Handler, UnimplementedSvcAuthServiceServer{}},
		{"AuthService_GetChallenge", _SvcAuthService_GetChallenge_Handler, UnimplementedSvcAuthServiceServer{}},
		{"CacheService_Get", _CacheService_Get_Handler, UnimplementedCacheServiceServer{}},
		{"CacheService_Set", _CacheService_Set_Handler, UnimplementedCacheServiceServer{}},
		{"CacheService_Delete", _CacheService_Delete_Handler, UnimplementedCacheServiceServer{}},
		{"CacheService_Clear", _CacheService_Clear_Handler, UnimplementedCacheServiceServer{}},
		{"TranscoderService_SubmitJob", _TranscoderService_SubmitJob_Handler, UnimplementedTranscoderServiceServer{}},
		{"TranscoderService_GetJobStatus", _TranscoderService_GetJobStatus_Handler, UnimplementedTranscoderServiceServer{}},
		{"TranscoderService_CancelJob", _TranscoderService_CancelJob_Handler, UnimplementedTranscoderServiceServer{}},
		{"WorkerService_SubmitJob", _WorkerService_SubmitJob_Handler, UnimplementedWorkerServiceServer{}},
		{"WorkerService_GetJobStatus", _WorkerService_GetJobStatus_Handler, UnimplementedWorkerServiceServer{}},
		{"WorkerService_CancelJob", _WorkerService_CancelJob_Handler, UnimplementedWorkerServiceServer{}},
		{"WorkerService_ScheduleJob", _WorkerService_ScheduleJob_Handler, UnimplementedWorkerServiceServer{}},
		{"MonitorService_GetHealth", _MonitorService_GetHealth_Handler, UnimplementedMonitorServiceServer{}},
		{"MonitorService_GetMetrics", _MonitorService_GetMetrics_Handler, UnimplementedMonitorServiceServer{}},
		{"MonitorService_GetAlerts", _MonitorService_GetAlerts_Handler, UnimplementedMonitorServiceServer{}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.handler(tc.srv, context.Background(), noopDec, interceptor)
			require.Error(t, err)
			assert.Equal(t, codes.Unimplemented, status.Code(err))
		})
	}
}

func TestHandler_DecodeError(t *testing.T) {
	decErr := func(interface{}) error { return status.Errorf(codes.Internal, "decode failed") }

	_, err := _HealthService_Check_Handler(UnimplementedHealthServiceServer{}, context.Background(), decErr, nil)
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
}

func TestHandler_NoInterceptor(t *testing.T) {
	_, err := _HealthService_Check_Handler(UnimplementedHealthServiceServer{}, context.Background(), func(in interface{}) error { return nil }, nil)
	require.Error(t, err)
	assert.Equal(t, codes.Unimplemented, status.Code(err))
}

func TestHealthService_Watch_Unimplemented(t *testing.T) {
	server := UnimplementedHealthServiceServer{}
	err := server.Watch(&HealthCheckRequest{}, nil)
	require.Error(t, err)
	assert.Equal(t, codes.Unimplemented, status.Code(err))
}

func TestCacheService_Clear_Unimplemented(t *testing.T) {
	server := UnimplementedCacheServiceServer{}
	_, err := server.Clear(context.Background(), &emptypb.Empty{})
	require.Error(t, err)
	assert.Equal(t, codes.Unimplemented, status.Code(err))
}

func TestRawDescGZIP(t *testing.T) {
	data := file_proto_v1_service_proto_rawDescGZIP()
	assert.NotEmpty(t, data)

	data2 := file_proto_v1_service_proto_rawDescGZIP()
	assert.Equal(t, data, data2)
}

func TestServiceDescs_AllServices(t *testing.T) {
	descs := []*grpc.ServiceDesc{
		&HealthService_ServiceDesc,
		&SvcUploadService_ServiceDesc,
		&SvcStreamingService_ServiceDesc,
		&MetadataService_ServiceDesc,
		&SvcAuthService_ServiceDesc,
		&CacheService_ServiceDesc,
		&TranscoderService_ServiceDesc,
		&WorkerService_ServiceDesc,
		&MonitorService_ServiceDesc,
	}
	for _, desc := range descs {
		assert.NotEmpty(t, desc.ServiceName)
	}
}

func TestProtoReflect_NilMessage(t *testing.T) {
	var nilReq *HealthCheckRequest
	pr := nilReq.ProtoReflect()
	assert.NotNil(t, pr)

	var nilResp *HealthCheckResponse
	pr = nilResp.ProtoReflect()
	assert.NotNil(t, pr)
}
