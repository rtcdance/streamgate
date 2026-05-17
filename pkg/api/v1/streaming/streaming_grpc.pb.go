package streamingv1

import (
	context "context"

	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
)

type StreamingServiceClient interface {
	GetStreamURL(ctx context.Context, in *GetStreamURLRequest, opts ...grpc.CallOption) (*GetStreamURLResponse, error)
	GetManifest(ctx context.Context, in *GetManifestRequest, opts ...grpc.CallOption) (*GetManifestResponse, error)
	GetSegment(ctx context.Context, in *GetSegmentRequest, opts ...grpc.CallOption) (*GetSegmentResponse, error)
	StartStream(ctx context.Context, in *StartStreamRequest, opts ...grpc.CallOption) (*StartStreamResponse, error)
	StopStream(ctx context.Context, in *StopStreamRequest, opts ...grpc.CallOption) (*StopStreamResponse, error)
	GetStreamStats(ctx context.Context, in *GetStreamStatsRequest, opts ...grpc.CallOption) (*GetStreamStatsResponse, error)
}

type streamingServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewStreamingServiceClient(cc grpc.ClientConnInterface) StreamingServiceClient {
	return &streamingServiceClient{cc}
}

func (c *streamingServiceClient) GetStreamURL(ctx context.Context, in *GetStreamURLRequest, opts ...grpc.CallOption) (*GetStreamURLResponse, error) {
	out := new(GetStreamURLResponse)
	err := c.cc.Invoke(ctx, "/streaming.v1.StreamingService/GetStreamURL", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *streamingServiceClient) GetManifest(ctx context.Context, in *GetManifestRequest, opts ...grpc.CallOption) (*GetManifestResponse, error) {
	out := new(GetManifestResponse)
	err := c.cc.Invoke(ctx, "/streaming.v1.StreamingService/GetManifest", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *streamingServiceClient) GetSegment(ctx context.Context, in *GetSegmentRequest, opts ...grpc.CallOption) (*GetSegmentResponse, error) {
	out := new(GetSegmentResponse)
	err := c.cc.Invoke(ctx, "/streaming.v1.StreamingService/GetSegment", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *streamingServiceClient) StartStream(ctx context.Context, in *StartStreamRequest, opts ...grpc.CallOption) (*StartStreamResponse, error) {
	out := new(StartStreamResponse)
	err := c.cc.Invoke(ctx, "/streaming.v1.StreamingService/StartStream", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *streamingServiceClient) StopStream(ctx context.Context, in *StopStreamRequest, opts ...grpc.CallOption) (*StopStreamResponse, error) {
	out := new(StopStreamResponse)
	err := c.cc.Invoke(ctx, "/streaming.v1.StreamingService/StopStream", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *streamingServiceClient) GetStreamStats(ctx context.Context, in *GetStreamStatsRequest, opts ...grpc.CallOption) (*GetStreamStatsResponse, error) {
	out := new(GetStreamStatsResponse)
	err := c.cc.Invoke(ctx, "/streaming.v1.StreamingService/GetStreamStats", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

type StreamingServiceServer interface {
	GetStreamURL(context.Context, *GetStreamURLRequest) (*GetStreamURLResponse, error)
	GetManifest(context.Context, *GetManifestRequest) (*GetManifestResponse, error)
	GetSegment(context.Context, *GetSegmentRequest) (*GetSegmentResponse, error)
	StartStream(context.Context, *StartStreamRequest) (*StartStreamResponse, error)
	StopStream(context.Context, *StopStreamRequest) (*StopStreamResponse, error)
	GetStreamStats(context.Context, *GetStreamStatsRequest) (*GetStreamStatsResponse, error)
	mustEmbedUnimplementedStreamingServiceServer()
}

type UnimplementedStreamingServiceServer struct{}

func (UnimplementedStreamingServiceServer) GetStreamURL(context.Context, *GetStreamURLRequest) (*GetStreamURLResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetStreamURL not implemented")
}
func (UnimplementedStreamingServiceServer) GetManifest(context.Context, *GetManifestRequest) (*GetManifestResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetManifest not implemented")
}
func (UnimplementedStreamingServiceServer) GetSegment(context.Context, *GetSegmentRequest) (*GetSegmentResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetSegment not implemented")
}
func (UnimplementedStreamingServiceServer) StartStream(context.Context, *StartStreamRequest) (*StartStreamResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method StartStream not implemented")
}
func (UnimplementedStreamingServiceServer) StopStream(context.Context, *StopStreamRequest) (*StopStreamResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method StopStream not implemented")
}
func (UnimplementedStreamingServiceServer) GetStreamStats(context.Context, *GetStreamStatsRequest) (*GetStreamStatsResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetStreamStats not implemented")
}
func (UnimplementedStreamingServiceServer) mustEmbedUnimplementedStreamingServiceServer() {}

type UnsafeStreamingServiceServer interface {
	mustEmbedUnimplementedStreamingServiceServer()
}

func RegisterStreamingServiceServer(s grpc.ServiceRegistrar, srv StreamingServiceServer) {
	s.RegisterService(&StreamingService_ServiceDesc, srv)
}

func _StreamingService_GetStreamURL_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetStreamURLRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(StreamingServiceServer).GetStreamURL(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/streaming.v1.StreamingService/GetStreamURL",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(StreamingServiceServer).GetStreamURL(ctx, req.(*GetStreamURLRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _StreamingService_GetManifest_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetManifestRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(StreamingServiceServer).GetManifest(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/streaming.v1.StreamingService/GetManifest",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(StreamingServiceServer).GetManifest(ctx, req.(*GetManifestRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _StreamingService_GetSegment_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetSegmentRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(StreamingServiceServer).GetSegment(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/streaming.v1.StreamingService/GetSegment",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(StreamingServiceServer).GetSegment(ctx, req.(*GetSegmentRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _StreamingService_StartStream_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(StartStreamRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(StreamingServiceServer).StartStream(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/streaming.v1.StreamingService/StartStream",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(StreamingServiceServer).StartStream(ctx, req.(*StartStreamRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _StreamingService_StopStream_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(StopStreamRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(StreamingServiceServer).StopStream(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/streaming.v1.StreamingService/StopStream",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(StreamingServiceServer).StopStream(ctx, req.(*StopStreamRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _StreamingService_GetStreamStats_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetStreamStatsRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(StreamingServiceServer).GetStreamStats(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/streaming.v1.StreamingService/GetStreamStats",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(StreamingServiceServer).GetStreamStats(ctx, req.(*GetStreamStatsRequest))
	}
	return interceptor(ctx, in, info, handler)
}

var StreamingService_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "streaming.v1.StreamingService",
	HandlerType: (*StreamingServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "GetStreamURL",
			Handler:    _StreamingService_GetStreamURL_Handler,
		},
		{
			MethodName: "GetManifest",
			Handler:    _StreamingService_GetManifest_Handler,
		},
		{
			MethodName: "GetSegment",
			Handler:    _StreamingService_GetSegment_Handler,
		},
		{
			MethodName: "StartStream",
			Handler:    _StreamingService_StartStream_Handler,
		},
		{
			MethodName: "StopStream",
			Handler:    _StreamingService_StopStream_Handler,
		},
		{
			MethodName: "GetStreamStats",
			Handler:    _StreamingService_GetStreamStats_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "proto/v1/streaming.proto",
}