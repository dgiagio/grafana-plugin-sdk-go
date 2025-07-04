# Benchmark

To run the benchmark, follow these steps:

**Run the server in a terminal**:

```shell
$ go run *.go datasource1 # this simulates a datasource that emits frames like prometheus
```

or

```shell
$ go run *.go datasource2 # this simulates a datasource that emits frames like loki
```

**Run the client in another terminal**:

You should run the client once for each test, let's call it `$TEST`. There are two tests available:

1. query - Uses the classic `QueryData`
2. stream - Uses the proposed `QueryChunkedData`

```shell
$ go run *.go client -test $TEST
```

Once the test finishes, go to the server terminal and press enter. This will allow the server process to collect memory
statistics, write a pprof file and terminate. The pprof file will be written to `datasource[1|2].heap.pprof`. Make sure
to move it to a name that matches the test you just ran.

Example:

```shell
$ mv datasource1.heap.pprof datasource1.query.heap.pprof
```

## Analyze the results

There are several ways to analyze a pprof file. My favorite is to use go's builtin HTTP interface.

```shell
$ go tool pprof -http=":8000" datasource1.$TEST.heap.pprof
```
