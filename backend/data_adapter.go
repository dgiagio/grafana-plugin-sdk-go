package backend

import (
	"context"
	"errors"
	"fmt"

	"github.com/grafana/grafana-plugin-sdk-go/data"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"

	"github.com/grafana/grafana-plugin-sdk-go/experimental/status"
	"github.com/grafana/grafana-plugin-sdk-go/genproto/pluginv2"
)

const (
	errorSourceMetadataKey = "errorSource"
)

// dataSDKAdapter adapter between low level plugin protocol and SDK interfaces.
type dataSDKAdapter struct {
	queryDataHandler   QueryDataHandler
	chunkedDataHandler ChunkedDataHandler
}

// newDataSDKAdapter creates a new adapter between the plugin protocol and SDK interfaces.
// It handles both query data and stream data operations.
func newDataSDKAdapter(queryDataHandler QueryDataHandler, chunkedDataHandler ChunkedDataHandler) *dataSDKAdapter {
	return &dataSDKAdapter{
		queryDataHandler:   queryDataHandler,
		chunkedDataHandler: chunkedDataHandler,
	}
}

// QueryData handles incoming gRPC data requests by converting them to SDK format
// and passing them to the registered QueryDataHandler.
func (a *dataSDKAdapter) QueryData(ctx context.Context, req *pluginv2.QueryDataRequest) (*pluginv2.QueryDataResponse, error) {
	parsedReq := FromProto().QueryDataRequest(req)
	resp, err := a.queryDataHandler.QueryData(ctx, parsedReq)
	if err != nil {
		return nil, enrichWithErrorSourceInfo(err)
	}

	if resp == nil {
		return nil, errors.New("both response and error are nil, but one must be provided")
	}

	return ToProto().QueryDataResponse(resp)
}

// QueryChunkedData handles incoming gRPC stream data requests by converting them to SDK format
// and passing them to the registered ChunkedDataHandler.
func (a *dataSDKAdapter) QueryChunkedData(req *pluginv2.ChunkedDataRequest, stream grpc.ServerStreamingServer[pluginv2.ChunkedDataResponse]) error {
	ctx := stream.Context()
	parsedReq := FromProto().ChunkedDataRequest(req)
	writer := newChunkedDataWriter(stream)

	err := a.chunkedDataHandler.QueryChunkedData(ctx, parsedReq, writer)
	if err != nil {
		return enrichWithErrorSourceInfo(err)
	}

	return nil
}

// chunkedDataWriter implements the ChunkedDataWriter interface for gRPC streaming.
// It buffers data frames and manages efficient transmission to clients.
type chunkedDataWriter struct {
	stream grpc.ServerStreamingServer[pluginv2.ChunkedDataResponse]
	states map[string]*chunkingState
	count  int
}

// chunkingState maintains the chunking state of data frames for a specific refID.
type chunkingState struct {
	frames   []*data.Frame
	curFrame *data.Frame // Pointer to the most recently added frame

	// Error handling fields
	Error       error
	Status      Status
	ErrorSource ErrorSource
}

// markerFrame represents an empty data frame used as a delimiter to signal the start of a new frame.
// It helps both the sender and receiver manage frame boundaries during data streaming.
var markerFrame = data.NewFrame("")

func (st *chunkingState) addFrame(f *data.Frame) {
	st.frames = append(st.frames, markerFrame, f)
	st.curFrame = f
}

func (st *chunkingState) addRow(fields ...any) error {
	if st.curFrame == nil {
		return errors.New("no frame being processed, cannot add row")
	}

	// Check field count matches
	if len(fields) != len(st.curFrame.Fields) {
		return fmt.Errorf("field count mismatch: got %d, want %d", len(fields), len(st.curFrame.Fields))
	}

	st.curFrame.AppendRow(fields...)
	return nil
}

func (st *chunkingState) reset() {
	var curFrame *data.Frame
	var frames []*data.Frame

	if st.curFrame != nil {
		curFrame = st.curFrame.EmptyCopy()
		frames = []*data.Frame{curFrame}
	}

	*st = chunkingState{
		frames:   frames,
		curFrame: curFrame,
	}
}

// newChunkedDataWriter creates a new writer that handles sending chunked data over gRPC.
// It manages buffering and efficient transmission of frames to clients.
func newChunkedDataWriter(stream grpc.ServerStreamingServer[pluginv2.ChunkedDataResponse]) *chunkedDataWriter {
	return &chunkedDataWriter{
		stream: stream,
		states: map[string]*chunkingState{},
	}
}

func (w *chunkedDataWriter) WriteFrame(refID string, f *data.Frame) error {
	state := w.states[refID]
	if state == nil {
		state = &chunkingState{}
		w.states[refID] = state
	}
	f.RefID = refID
	state.addFrame(f)

	w.count += f.Rows()
	return w.maybeFlush()
}

func (w *chunkedDataWriter) WriteFrameRow(refID string, fields ...any) error {
	state := w.states[refID]
	if err := state.addRow(fields...); err != nil {
		return err
	}

	w.count++
	return w.maybeFlush()
}

func (w *chunkedDataWriter) WriteError(refID string, err error) error {
	state := w.states[refID]
	state.Error = err
	w.states[refID] = state

	w.count++
	return w.flush()
}

func (w *chunkedDataWriter) Close() error {
	return w.flush()
}

func (w *chunkedDataWriter) maybeFlush() error {
	const maxBatchSize = 1000 // can be tuned
	if w.count < maxBatchSize {
		return nil
	}
	return w.flush()
}

func (w *chunkedDataWriter) flush() error {
	if w.count == 0 {
		return nil
	}

	for refID, state := range w.states {
		errStr := ""
		if state.Error != nil {
			errStr = state.Error.Error()
		}

		resp := &pluginv2.ChunkedDataResponse{
			RefId:       refID,
			Frames:      make([][]byte, 0, len(state.frames)),
			Status:      int32(state.Status),
			Error:       errStr,
			ErrorSource: state.ErrorSource.String(),
		}

		for _, frame := range state.frames {
			encoded, err := frame.MarshalArrow()
			if err != nil {
				return err
			}
			resp.Frames = append(resp.Frames, encoded)
		}

		if err := w.stream.Send(resp); err != nil {
			return err
		}
	}

	// Reset state
	for _, state := range w.states {
		state.reset()
	}
	w.count = 0

	return nil
}

// enrichWithErrorSourceInfo returns a gRPC status error with error source info as metadata.
func enrichWithErrorSourceInfo(err error) error {
	var errorSource status.Source
	if IsDownstreamError(err) {
		errorSource = status.SourceDownstream
	} else if IsPluginError(err) {
		errorSource = status.SourcePlugin
	}

	// Unless the error is explicitly marked as a plugin or downstream error, we don't enrich it.
	if errorSource == "" {
		return err
	}

	status := grpcstatus.New(codes.Unknown, err.Error())
	status, innerErr := status.WithDetails(&errdetails.ErrorInfo{
		Metadata: map[string]string{
			errorSourceMetadataKey: errorSource.String(),
		},
	})
	if innerErr != nil {
		return err
	}

	return status.Err()
}

// HandleGrpcStatusError handles gRPC status errors by extracting the error source from the error details and injecting
// the error source into context.
func ErrorSourceFromGrpcStatusError(ctx context.Context, err error) (status.Source, bool) {
	st := grpcstatus.Convert(err)
	if st == nil {
		return status.DefaultSource, false
	}
	for _, detail := range st.Details() {
		if errorInfo, ok := detail.(*errdetails.ErrorInfo); ok {
			errorSource, exists := errorInfo.Metadata[errorSourceMetadataKey]
			if !exists {
				break
			}

			switch errorSource {
			case string(ErrorSourceDownstream):
				innerErr := WithErrorSource(ctx, ErrorSourceDownstream)
				if innerErr != nil {
					Logger.Error("Could not set downstream error source", "error", innerErr)
				}
				return status.SourceDownstream, true
			case string(ErrorSourcePlugin):
				errorSourceErr := WithErrorSource(ctx, ErrorSourcePlugin)
				if errorSourceErr != nil {
					Logger.Error("Could not set plugin error source", "error", errorSourceErr)
				}
				return status.SourcePlugin, true
			}
		}
	}
	return status.DefaultSource, false
}
