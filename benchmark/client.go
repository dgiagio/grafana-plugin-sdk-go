package main

import (
	"context"
	"encoding/json"
	"flag"
	"os"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	experimentalDS "github.com/grafana/grafana-plugin-sdk-go/experimental/datasourcetest"
	"github.com/grafana/grafana-plugin-sdk-go/genproto/pluginv2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func runClient() {
	var testName string
	flag.StringVar(&testName, "test", "", "test name")
	flag.Parse()

	if testName == "" {
		usage()
	}

	c, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithMaxMsgSize(1024*1024*1024))
	panicIfErr(err)
	defer c.Close()

	ctx := context.Background()
	client := &experimentalDS.TestPluginClient{DataClient: pluginv2.NewDataClient(c)}
	pCtx := backend.PluginContext{DataSourceInstanceSettings: &backend.DataSourceInstanceSettings{}}

	var resp *backend.QueryDataResponse
	switch testName {
	case "query":
		resp = queryData(ctx, pCtx, client)
	case "stream":
		resp = queryChunkedData(ctx, pCtx, client)
	}

	respJSON, err := json.Marshal(resp)
	panicIfErr(err)

	err = os.WriteFile(testName+".resp.json", respJSON, 0644)
	panicIfErr(err)
}

func queryData(ctx context.Context, pCtx backend.PluginContext, client *experimentalDS.TestPluginClient) *backend.QueryDataResponse {
	qdr := &backend.QueryDataRequest{
		PluginContext: pCtx,
		Queries: []backend.DataQuery{
			{RefID: "1", QueryType: "test1", MaxDataPoints: 10},
			{RefID: "2", QueryType: "test2", MaxDataPoints: 100},
			//{RefID: "3", QueryType: "test3", MaxDataPoints: 1e3},
			//{RefID: "4", QueryType: "test3", MaxDataPoints: 1e5},
		},
	}

	resp, err := client.QueryData(ctx, qdr)
	panicIfErr(err)
	return resp
}

func queryChunkedData(ctx context.Context, pCtx backend.PluginContext, client *experimentalDS.TestPluginClient) *backend.QueryDataResponse {
	sdr := &backend.ChunkedDataRequest{
		PluginContext: pCtx,
		Queries: []backend.DataQuery{
			{RefID: "1", QueryType: "test1", MaxDataPoints: 10},
			{RefID: "2", QueryType: "test2", MaxDataPoints: 100},
			//{RefID: "3", QueryType: "test3", MaxDataPoints: 1e3},
			//{RefID: "4", QueryType: "test3", MaxDataPoints: 1e5},
		},
	}
	resp, err := client.QueryChunkedData(ctx, sdr)
	panicIfErr(err)
	return resp
}
