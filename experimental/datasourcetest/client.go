package datasourcetest

import (
	"context"
	"errors"
	"io"

	"github.com/grafana/grafana-plugin-sdk-go/data"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/genproto/pluginv2"
)

type TestPluginClient struct {
	DataClient        pluginv2.DataClient
	DiagnosticsClient pluginv2.DiagnosticsClient
	ResourceClient    pluginv2.ResourceClient

	conn *grpc.ClientConn
}

func newTestPluginClient(addr string) (*TestPluginClient, error) {
	c, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithMaxMsgSize(1024*1024*1024))
	if err != nil {
		return nil, err
	}

	return &TestPluginClient{
		conn:              c,
		DiagnosticsClient: pluginv2.NewDiagnosticsClient(c),
		DataClient:        pluginv2.NewDataClient(c),
		ResourceClient:    pluginv2.NewResourceClient(c),
	}, nil
}

func (p *TestPluginClient) QueryData(ctx context.Context, r *backend.QueryDataRequest) (*backend.QueryDataResponse, error) {
	req := backend.ToProto().QueryDataRequest(r)

	resp, err := p.DataClient.QueryData(ctx, req)
	if err != nil {
		return nil, err
	}

	return backend.FromProto().QueryDataResponse(resp)
}

func (p *TestPluginClient) QueryChunkedData(ctx context.Context, r *backend.ChunkedDataRequest) (*backend.QueryDataResponse, error) {
	req := backend.ToProto().ChunkedDataRequest(r)

	stream, err := p.DataClient.QueryChunkedData(ctx, req)
	if err != nil {
		return nil, err
	}

	type streamState struct {
		frames   []*data.Frame
		curFrame *data.Frame
	}

	stateByRefID := make(map[string]streamState)

	for {
		sr, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {

				resp := backend.Responses{}
				for refID, state := range stateByRefID {
					resp[refID] = backend.DataResponse{
						Frames: state.frames,
					}
				}

				// End of stream, return accumulated responses
				return &backend.QueryDataResponse{Responses: resp}, nil
			}
			return nil, err
		}

		st := stateByRefID[sr.RefId]
		for _, frame := range sr.Frames {
			f, err := data.UnmarshalArrowFrame(frame)
			if err != nil {
				return nil, err
			}

			if f.Rows() == 0 {
				st.curFrame = nil
				continue
			}

			if st.curFrame != nil {
				// Merge current frame with incoming frame
				for i, field := range f.Fields {
					st.curFrame.Fields[i].AppendAll(field)
				}
				continue
			}

			// This is a new frame
			st.frames = append(st.frames, f)
			st.curFrame = f
		}

		stateByRefID[sr.RefId] = st
	}
}

func (p *TestPluginClient) CheckHealth(ctx context.Context, r *backend.CheckHealthRequest) (*backend.CheckHealthResult, error) {
	req := &pluginv2.CheckHealthRequest{
		PluginContext: backend.ToProto().PluginContext(r.PluginContext),
	}

	resp, err := p.DiagnosticsClient.CheckHealth(ctx, req)
	if err != nil {
		return nil, err
	}

	return backend.FromProto().CheckHealthResponse(resp), nil
}

func (p *TestPluginClient) CallResource(ctx context.Context, r *backend.CallResourceRequest, sender backend.CallResourceResponseSender) error {
	protoReq := backend.ToProto().CallResourceRequest(r)
	protoStream, err := p.ResourceClient.CallResource(ctx, protoReq)
	if err != nil {
		if status.Code(err) == codes.Unimplemented {
			return errors.New("method not implemented")
		}

		return err
	}

	for {
		protoResp, err := protoStream.Recv()
		if err != nil {
			if status.Code(err) == codes.Unimplemented {
				return errors.New("method not implemented")
			}

			if errors.Is(err, io.EOF) {
				return nil
			}

			return err
		}

		if err = sender.Send(backend.FromProto().CallResourceResponse(protoResp)); err != nil {
			return err
		}
	}
}

func (p *TestPluginClient) shutdown() error {
	return p.conn.Close()
}
