package service

import "github.com/rtcdance/streamgate/pkg/service/streamingsvc"

type StreamingService = streamingsvc.StreamingService
type Quality = streamingsvc.Quality
type StreamInfo = streamingsvc.StreamInfo

var NewStreamingService = streamingsvc.NewStreamingService
var DetectStreamType = streamingsvc.DetectStreamType
var BuildMediaPlaylist = streamingsvc.BuildMediaPlaylist
