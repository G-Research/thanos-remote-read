// Binary thanos-remote-read provides an adapter from Prometheus remote read
// protocol to Thanos StoreAPI.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"sort"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/gogo/protobuf/proto"
	"github.com/golang/snappy"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/prometheus/prompb"
	"github.com/prometheus/prometheus/tsdb/chunkenc"
	"github.com/thanos-io/thanos/pkg/store/storepb"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"

	grpc_prometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
	jaegerPropagator "go.opentelemetry.io/contrib/propagators/jaeger"
	jaegerExporter "go.opentelemetry.io/otel/exporters/trace/jaeger"
)

var (
	flagListen         = flag.String("listen", ":10080", "[ip]:port to serve HTTP on")
	flagStore          = flag.String("store", "localhost:10901", "Thanos Store API gRPC endpoint")
	flagIgnoreWarnings = flag.Bool("ignore-warnings", false, "Ignore warnings from Thanos")
	flagLogFormat      = flag.String("log.format", "logfmt", "Log format. One of [logfmt, json]")
	flagLogLevel       = flag.String("log.level", "info", "Log filtering level. One of [debug, info, warn, error]")
)

var (
	httpRequests = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "http",
			Name:      "requests_total",
		},
		[]string{"code", "method", "handler"})
)

func init() {
	prometheus.MustRegister(httpRequests)
}

func initTracer(logger log.Logger) func() {
	flush, err := jaegerExporter.InstallNewPipeline(
		jaegerExporter.WithCollectorEndpoint(""),
		jaegerExporter.WithProcess(jaegerExporter.Process{
			ServiceName: "thanos-remote-read",
		}),
		jaegerExporter.WithDisabled(true),
		jaegerExporter.WithDisabledFromEnv(),
	)
	if err != nil {
		if logErr := level.Error(logger).Log("err", err); logErr != nil {
			fmt.Fprintf(os.Stderr, "original error: %v, logging error: %v\n", err, logErr)
		}
		os.Exit(1)
	}

	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		jaegerPropagator.Jaeger{},
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return flush
}

func NewConfiguredLogger(format string, logLevel string) (log.Logger, error) {
	var logger log.Logger
	switch format {
	case "logfmt":
		logger = log.NewLogfmtLogger(os.Stdout)
	case "json":
		logger = log.NewJSONLogger(os.Stdout)
	default:
		return nil, fmt.Errorf("%s is not a valid log format", format)
	}

	var filterOption level.Option
	switch logLevel {
	case "debug":
		filterOption = level.AllowDebug()
	case "info":
		filterOption = level.AllowInfo()
	case "warn":
		filterOption = level.AllowWarn()
	case "error":
		filterOption = level.AllowError()
	default:
		return nil, fmt.Errorf("%s is not a valid log level", logLevel)
	}
	logger = log.With(logger, "ts", log.DefaultTimestampUTC, "caller", log.Caller(5))
	logger = level.NewFilter(logger, filterOption)
	return logger, nil
}

func main() {
	fmt.Println("info: starting up thanos-remote-read...")
	flag.Parse()

	logger, err := NewConfiguredLogger(*flagLogFormat, *flagLogLevel)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Could not initialize logger: %s", err)
		os.Exit(1)
	}

	flush := initTracer(logger)
	defer flush()

	conn, err := grpc.Dial(*flagStore, grpc.WithInsecure(),
		grpc.WithUnaryInterceptor(grpc_prometheus.UnaryClientInterceptor),
		grpc.WithUnaryInterceptor(otelgrpc.UnaryClientInterceptor()),
		grpc.WithStreamInterceptor(grpc_prometheus.StreamClientInterceptor),
		grpc.WithStreamInterceptor(otelgrpc.StreamClientInterceptor()))
	if err != nil {
		if logErr := level.Error(logger).Log("err", err); logErr != nil {
			fmt.Fprintf(os.Stderr, "original error: %v, logging error: %v\n", err, logErr)
		}
		os.Exit(1)
	}
	setup(conn, logger)
	err = (http.ListenAndServe(*flagListen, nil))
	if err != nil {
		if logErr := level.Error(logger).Log("err", err); logErr != nil {
			fmt.Fprintf(os.Stderr, "original error: %v, logging error: %v\n", err, logErr)
		}
		os.Exit(1)
	}
}

func setup(conn *grpc.ClientConn, logger log.Logger) {
	api := &API{
		client: storepb.NewStoreClient(conn),
	}

	handler := func(path, name string, f http.HandlerFunc) {
		http.HandleFunc(path, promhttp.InstrumentHandlerCounter(
			httpRequests.MustCurryWith(prometheus.Labels{"handler": name}),
			otelhttp.NewHandler(f, name),
		))
	}
	handler("/", "root", root)
	handler("/-/healthy", "health", ok)
	handler("/api/v1/read", "read", errorWrap(loggerWrap(api.remoteRead, logger)))

	http.Handle("/metrics", promhttp.Handler())
}

type API struct {
	client storepb.StoreClient
}

func loggerWrap(f func(w http.ResponseWriter, r *http.Request, logger log.Logger) error, logger log.Logger) func(w http.ResponseWriter, r *http.Request) error {
	return func(w http.ResponseWriter, r *http.Request) error {
		return f(w, r, logger)
	}
}

func errorWrap(f func(w http.ResponseWriter, r *http.Request) error) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		err := f(w, r)
		if err != nil {
			if httpErr, ok := err.(HTTPError); ok {
				http.Error(w, httpErr.Error(), httpErr.Status)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
}

type HTTPError struct {
	error
	Status int
}

func (api *API) remoteRead(w http.ResponseWriter, r *http.Request, logger log.Logger) error {
    ctx := r.Context()
    tracer := otel.Tracer("")
    var span trace.Span
    ctx, span = tracer.Start(ctx, "remoteRead")
    defer span.End()

    compressed, err := ioutil.ReadAll(r.Body)
    if err != nil {
        return err
    }

    reqBuf, err := snappy.Decode(nil, compressed)
    if err != nil {
        return HTTPError{err, http.StatusBadRequest}
    }

    var req prompb.ReadRequest
    if err := proto.Unmarshal(reqBuf, &req); err != nil {
        return HTTPError{err, http.StatusBadRequest}
    }

    ignoredSelector := make(map[string]struct{})
    if ignores, ok := r.URL.Query()["ignore"]; ok {
        for _, ignore := range ignores {
            ignoredSelector[ignore] = struct{}{}
        }
    }

    // Use the updated context `ctx` that includes the span for tracing.
    resp, err := api.doStoreRequest(ctx, &req, ignoredSelector, logger)
    if err != nil {
        return err
    }

    data, err := proto.Marshal(resp)
    if err != nil {
        return err
    }

    w.Header().Set("Content-Type", "application/x-protobuf")
    w.Header().Set("Content-Encoding", "snappy")

    compressed = snappy.Encode(nil, data)
    if _, err := w.Write(compressed); err != nil {
        if logErr := level.Error(logger).Log("err", err, "traceID", span.SpanContext().TraceID); logErr != nil {
            fmt.Fprintf(os.Stderr, "original error: %v, logging error: %v\n", err, logErr)
        }
        os.Exit(1)
    }
    return nil
}

var promMatcherToThanos = map[prompb.LabelMatcher_Type]storepb.LabelMatcher_Type{
	prompb.LabelMatcher_EQ:  storepb.LabelMatcher_EQ,
	prompb.LabelMatcher_NEQ: storepb.LabelMatcher_NEQ,
	prompb.LabelMatcher_RE:  storepb.LabelMatcher_RE,
	prompb.LabelMatcher_NRE: storepb.LabelMatcher_NRE,
}

type AggrChunkByTimestamp []storepb.AggrChunk

func (c AggrChunkByTimestamp) Len() int           { return len(c) }
func (c AggrChunkByTimestamp) Swap(i, j int)      { c[i], c[j] = c[j], c[i] }
func (c AggrChunkByTimestamp) Less(i, j int) bool { return c[i].MinTime < c[j].MinTime }

func (api *API) doStoreRequest(ctx context.Context, req *prompb.ReadRequest, ignoredSelector map[string]struct{}, logger log.Logger) (*prompb.ReadResponse, error) {
	tracer := otel.Tracer("")
	var span trace.Span
	ctx, span = tracer.Start(ctx, "doStoreRequest")
	defer span.End()

	response := &prompb.ReadResponse{}

	for _, query := range req.Queries {
		storeReq := &storepb.SeriesRequest{
			MinTime: query.StartTimestampMs,
			MaxTime: query.EndTimestampMs,
			// Prometheus doesn't understand Thanos compaction, only ask for raw data.
			Aggregates: []storepb.Aggr{storepb.Aggr_RAW},
			Matchers:   make([]storepb.LabelMatcher, 0, len(query.Matchers)),
		}
		for _, matcher := range query.Matchers {
			if _, ok := ignoredSelector[matcher.Name]; ok {
				continue
			}
			storeReq.Matchers = append(storeReq.Matchers, storepb.LabelMatcher{
				Name:  matcher.Name,
				Type:  promMatcherToThanos[matcher.Type],
				Value: matcher.Value})
		}

		if logErr := level.Info(logger).Log(
			"traceID", span.SpanContext().TraceID,
			"msg", "thanos request",
			"request", fmt.Sprintf("%v", storeReq),
		); logErr != nil {
			fmt.Fprintf(os.Stderr, "logging error: %v\n", logErr)
		}
		storeRes, err := api.client.Series(ctx, storeReq)
		if err != nil {
			return nil, err
		}

		result := &prompb.QueryResult{}
		iter := chunkenc.NewNopIterator()

		for {
			res, err := storeRes.Recv()
			if err == io.EOF {
				break
			}
			if err != nil {
				if logErr := level.Error(logger).Log("err", err, "traceID", span.SpanContext().TraceID, "msg", "Error in recv from thanos"); logErr != nil {
					fmt.Fprintf(os.Stderr, "failed to log error: %v\n", logErr)
				}
				return nil, err
			}

			switch r := res.GetResult().(type) {
			case *storepb.SeriesResponse_Series:
				t := &prompb.TimeSeries{}
				for _, label := range r.Series.Labels {
					t.Labels = append(t.Labels, prompb.Label{
						Name:  label.Name,
						Value: label.Value,
					})
				}

				sort.Sort(AggrChunkByTimestamp(r.Series.Chunks))
				for _, chunk := range r.Series.Chunks {
					if chunk.Raw == nil {
						// We only ask for and handle RAW
						err := fmt.Errorf("unexpectedly missing raw chunk data")
						if logErr := level.Error(logger).Log("err", err, "traceID", span.SpanContext().TraceID); logErr != nil {
							fmt.Fprintf(os.Stderr, "logging error: %v\n", logErr)
						}
						return nil, err
					}
					if chunk.Raw.Type != storepb.Chunk_XOR {
						err := fmt.Errorf("unexpected encoding type: %v", chunk.Raw.Type)
						_ = level.Error(logger).Log("err", err, "traceID", span.SpanContext().TraceID)
						return nil, err
					}

					raw, err := chunkenc.FromData(chunkenc.EncXOR, chunk.Raw.Data)
					if err != nil {
						err = fmt.Errorf("reading chunk: %w", err)
						if logErr := level.Error(logger).Log("err", err, "traceID", span.SpanContext().TraceID); logErr != nil {
							fmt.Fprintf(os.Stderr, "failed to log error: %v\n", logErr)
						}
						return nil, err
					}

					iter = raw.Iterator(iter)
					for iter.Next() {
						ts, value := iter.At()
						t.Samples = append(t.Samples, prompb.Sample{
							Timestamp: ts,
							Value:     value,
						})
					}
				}

				result.Timeseries = append(result.Timeseries, t)

			case *storepb.SeriesResponse_Warning:
				if *flagIgnoreWarnings {
					if logErr := level.Warn(logger).Log("result", fmt.Sprintf("%v", r), "traceID", span.SpanContext().TraceID); logErr != nil {
						fmt.Fprintf(os.Stderr, "logging warning: %v\n", logErr)
					}
				} else {
					return nil, HTTPError{fmt.Errorf("%v", r), http.StatusInternalServerError}
				}
			}
		}
		response.Results = append(response.Results, result)
	}
	return response, nil
}

func ok(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	span := trace.SpanFromContext(ctx)
	defer span.End()
	_, _ = w.Write([]byte("ok"))
}

func root(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	span := trace.SpanFromContext(ctx)
	defer span.End()
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-type", "text/html")
	_, _ = w.Write([]byte(`
	<p>thanos-remote-read adapter</p>
	<ul>
		<li><a href="/-/healthy">/-/healthy</a>
		<li><a href="/metrics">/metrics</a>
		<li>/api/v1/read (point Prometheus here!)
	</ul>`))
}
