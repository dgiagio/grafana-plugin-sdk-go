package main

import (
	"fmt"
	"os"
	"runtime"
	"runtime/pprof"

	"github.com/grafana/grafana-plugin-sdk-go/backend/log"
)

var logger = log.NewWithLevel(log.Info)

const addr = "127.0.0.1:28590" // random port

func usage() {
	fmt.Println("Usage: prog datasource1")
	fmt.Println("       prog datasource2")
	fmt.Println("       prog client -test <query|stream>")
	fmt.Println("")
	fmt.Println("Datasource implementations:")
	fmt.Println("  datasource1 - Test datasource that returns many data frames (e.g. prometheus)")
	fmt.Println("  datasource2 - Test datasource that returns few data frames with many rows each (e.g. loki)")
	fmt.Println("")
	os.Exit(1)
}

func main() {
	if len(os.Args) < 2 {
		usage()
	}

	mode := os.Args[1]
	os.Args = os.Args[1:]

	switch mode {
	case "datasource1":
		runDatasource1()
	case "datasource2":
		runDatasource2()
	case "client":
		runClient()
	default:
		usage()
	}
}

func collectMemProfile(testName string) {
	f, err := os.Create(testName + ".heap.pprof")
	panicIfErr(err)

	runtime.GC() // materialize all statistics

	if err = pprof.WriteHeapProfile(f); err != nil {
		panic("can't write heap profile")
	}

	_ = f.Close()
}

func panicIfErr(err error) {
	if err != nil {
		panic(err)
	}
}
