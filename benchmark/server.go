package main

import (
	"context"
	"fmt"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/datasource"
	"github.com/grafana/grafana-plugin-sdk-go/backend/instancemgmt"
	"github.com/grafana/grafana-plugin-sdk-go/data"
	experimentalDS "github.com/grafana/grafana-plugin-sdk-go/experimental/datasourcetest"
)

type testDatasource struct {
	rowsPerFrame int
}

func runDatasource(name string, ds testDatasource) {
	logger.Info("Listening on", "addr", addr)

	factory := datasource.InstanceFactoryFunc(func(_ context.Context, _ backend.DataSourceInstanceSettings) (instancemgmt.Instance, error) {
		return &ds, nil
	})

	p, err := experimentalDS.Manage(factory, experimentalDS.ManageOpts{
		Address:           addr,
		MaxReceiveMsgSize: 256 * 1024 * 1024, // 256MB
	})
	panicIfErr(err)

	// Wait for a key press
	var key int
	_, _ = fmt.Scanln(&key)

	collectMemProfile(name)

	_ = p.Shutdown()
}

func runDatasource1() {
	runDatasource("datasource1", testDatasource{
		rowsPerFrame: 10,
	})
}

func runDatasource2() {
	runDatasource("datasource2", testDatasource{
		rowsPerFrame: 100_000,
	})
}

func (p *testDatasource) QueryData(ctx context.Context, req *backend.QueryDataRequest) (*backend.QueryDataResponse, error) {
	logger.Info("QueryData", "req", req)

	query := func(ctx context.Context, pCtx backend.PluginContext, q backend.DataQuery) backend.DataResponse {
		frames := make([]*data.Frame, 0, q.MaxDataPoints)
		for i := range q.MaxDataPoints {
			frame := newFrame(fmt.Sprintf("F:%d, R:%s", i, q.RefID), p.rowsPerFrame)
			frames = append(frames, frame)
		}

		return backend.DataResponse{
			Status: backend.StatusOK,
			Frames: frames,
		}
	}

	response := backend.NewQueryDataResponse()

	// Loop over queries and execute them individually.
	for _, q := range req.Queries {
		res := query(ctx, req.PluginContext, q)
		response.Responses[q.RefID] = res
	}

	return response, nil
}

func (p *testDatasource) QueryChunkedData(ctx context.Context, req *backend.ChunkedDataRequest, w backend.ChunkedDataWriter) error {
	logger.Info("queryChunkedData", "req", req)

	query := func(ctx context.Context, pCtx backend.PluginContext, q backend.DataQuery) {
		for i := range q.MaxDataPoints {
			frame := newFrame(fmt.Sprintf("F:%d, R:%s", i, q.RefID), 0)
			err := w.WriteFrame(q.RefID, frame)
			panicIfErr(err)
			for i := 0; i < p.rowsPerFrame; i++ {
				err = w.WriteFrameRow(q.RefID,
					float32(i),
					float64(i),
					"str",
					int8(i),
					int16(i),
					int32(i),
					int64(i),
					uint8(i),
					uint16(i),
					uint32(i),
					uint64(i),
					time.UnixMilli(int64(i)),
					false,
				)
				panicIfErr(err)
			}
		}
	}

	// Loop over queries and execute them individually.
	for _, q := range req.Queries {
		query(ctx, req.PluginContext, q)
	}

	return w.Close()
}

func newFrame(name string, numRows int) *data.Frame {
	// Create frame
	frame := data.NewFrame(name, data.NewField("f32", nil, []float32{}),
		data.NewField("f64", nil, []float64{}),
		data.NewField("s", nil, []string{}),
		data.NewField("i8", nil, []int8{}),
		data.NewField("i16", nil, []int16{}),
		data.NewField("i32", nil, []int32{}),
		data.NewField("i64", nil, []int64{}),
		data.NewField("u8", nil, []uint8{}),
		data.NewField("u16", nil, []uint16{}),
		data.NewField("u32", nil, []uint32{}),
		data.NewField("u64", nil, []uint64{}),
		data.NewField("t", nil, []time.Time{}),
		data.NewField("b", nil, []bool{}))

	// Add the specified number of rows with sample data
	for i := 0; i < numRows; i++ {
		frame.AppendRow(
			float32(i),
			float64(i),
			"str",
			int8(i),
			int16(i),
			int32(i),
			int64(i),
			uint8(i),
			uint16(i),
			uint32(i),
			uint64(i),
			time.UnixMilli(int64(i)),
			false)
	}

	return frame
}
