// Binary thanos-remote-read provides an adapter from Prometheus remote read
// protocol to Thanos StoreAPI.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"sort"
	"strings"

	"github.com/gogo/protobuf/proto"
	"github.com/golang/snappy"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/prometheus/prompb"
	"github.com/prometheus/prometheus/tsdb/chunkenc"
	"github.com/thanos-io/thanos/pkg/store/storepb"
	"google.golang.org/grpc"

	grpc_prometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
)

var (
	flagListen        = flag.String("listen", ":10080", "[ip]:port to serve HTTP on")
	flagStore         = flag.String("store", "localhost:10901", "Thanos Store API gRPC endpoint")
	flagReplicaLabels = flag.String("replica-labels", "", "Replica labels")

	replicaLabels = map[string]bool{}
)

var (
	httpRequests = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "http",
			Name:      "requests_total",
		},
		[]string{"code", "method", "handler"})
	dedupedSeriesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "thanos_remote_read",
			Name:      "deduped_series_total",
		},
		[]string{"response_state"})
	dedupedSeriesSorted   = dedupedSeriesTotal.WithLabelValues("sorted")
	dedupedSeriesUnsorted = dedupedSeriesTotal.WithLabelValues("unsorted")
	seriesTotal           = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: "thanos_remote_read",
			Name:      "series_total",
		})
)

func init() {
	prometheus.MustRegister(httpRequests)
}

func main() {
	flag.Parse()

	if len(*flagReplicaLabels) > 0 {
		for _, label := range strings.Split(*flagReplicaLabels, ",") {
			replicaLabels[label] = true
		}
	}

	var err error
	conn, err := grpc.Dial(*flagStore, grpc.WithInsecure(),
		grpc.WithUnaryInterceptor(grpc_prometheus.UnaryClientInterceptor),
		grpc.WithStreamInterceptor(grpc_prometheus.StreamClientInterceptor))
	if err != nil {
		log.Fatal(err)
	}
	setup(conn)
	log.Fatal(http.ListenAndServe(*flagListen, nil))
}

func setup(conn *grpc.ClientConn) {
	api := &API{
		client: storepb.NewStoreClient(conn),
	}

	handler := func(path, name string, f http.HandlerFunc) {
		http.HandleFunc(path, promhttp.InstrumentHandlerCounter(
			httpRequests.MustCurryWith(prometheus.Labels{"handler": name}), f))
	}
	handler("/", "root", root)
	handler("/-/healthy", "health", ok)
	handler("/api/v1/read", "read", errorWrap(api.remoteRead))

	http.Handle("/metrics", promhttp.Handler())
}

type API struct {
	client storepb.StoreClient
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

func (api *API) remoteRead(w http.ResponseWriter, r *http.Request) error {
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

	// This does not do streaming, at the time of writing Prometheus doesn't ask
	// for it anyway: https://github.com/prometheus/prometheus/issues/5926

	resp, err := api.doStoreRequest(r.Context(), &req)
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
		log.Printf("Error writing response: %v", err)
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

type ReplicaSeries struct {
	storepb.Series
	ReplicaLabels, OtherLabels []storepb.Label
}

func (api *API) doStoreRequest(ctx context.Context, req *prompb.ReadRequest) (*prompb.ReadResponse, error) {
	response := &prompb.ReadResponse{}

	for _, query := range req.Queries {
		storeReq := &storepb.SeriesRequest{
			MinTime: query.StartTimestampMs,
			MaxTime: query.EndTimestampMs,
			// Prometheus doesn't understand Thanos compaction, only ask for raw data.
			Aggregates: []storepb.Aggr{storepb.Aggr_RAW},
			Matchers:   make([]storepb.LabelMatcher, len(query.Matchers)),
		}
		for i, matcher := range query.Matchers {
			storeReq.Matchers[i] = storepb.LabelMatcher{
				Name:  matcher.Name,
				Type:  promMatcherToThanos[matcher.Type],
				Value: matcher.Value}
		}

		log.Printf("Thanos request: %v", storeReq)
		storeRes, err := api.client.Series(ctx, storeReq)
		if err != nil {
			return nil, err
		}

		result := &prompb.QueryResult{}
		iter := chunkenc.NewNopIterator()
		series := []*ReplicaSeries{}
		sorted := true

		for {
			res, err := storeRes.Recv()
			if err == io.EOF {
				break
			}
			if err != nil {
				log.Printf("Error in recv from thanos: %v", err)
				return nil, err
			}

			switch r := res.GetResult().(type) {
			case *storepb.SeriesResponse_Series:
				// Series.Labels is a set of labels, they should be sorted, however we
				// rearrange them to be: (other labels), (replica labels)
				rl := make([]storepb.Label, 0, len(replicaLabels))
				labels := make([]storepb.Label, 0, len(r.Series.Labels))
				for _, l := range r.Series.Labels {
					if _, ok := replicaLabels[l.Name]; ok {
						rl = append(rl, l)
					} else {
						labels = append(labels, l)
					}
				}
				if len(rl) > 0 {
					labels = append(labels, rl...)
					r.Series.Labels = labels
					rl = labels[len(labels)-len(rl):]
					labels = labels[:len(labels)-len(rl)]
				}
				rs := &ReplicaSeries{Series: *r.Series, ReplicaLabels: rl, OtherLabels: labels}

				if len(series) > 0 {
					s := series[len(series)-1]
					cmp := storepb.CompareLabels(s.OtherLabels, rs.OtherLabels)
					if cmp > 0 {
						sorted = false
					} else if cmp == 0 {
						// Identical, need to merge.
						s.Chunks = sortChunks(s.Chunks, rs.Chunks)
						dedupedSeriesSorted.Add(1)
						continue
					}
				}

				sort.Sort(AggrChunkByTimestamp(r.Series.Chunks))

				series = append(series, rs)

			case *storepb.SeriesResponse_Warning:
				// TODO: Can we return this somehow?
				log.Printf("Warning from thanos: %v", r)
			}
		}

		if !sorted {
			sort.Slice(series, func(i, j int) bool {
				return storepb.CompareLabels(series[i].OtherLabels, series[j].OtherLabels) < 0
			})
			results := []*ReplicaSeries{}
			for i, rs := range series {
				if i == 0 {
					results = append(results, rs)
					continue
				}
				s := series[i-1]
				cmp := storepb.CompareLabels(s.OtherLabels, rs.OtherLabels)
				if cmp == 0 {
					// Identical, need to merge.
					s.Chunks = sortChunks(s.Chunks, rs.Chunks)
					dedupedSeriesUnsorted.Add(1)
					// Overwrite this series with the previous one, so deduplication of
					// multiple series works
					series[i] = s
				} else {
					results = append(results, rs)
				}
			}
			series = results
		}

		for _, series := range series {
			t := &prompb.TimeSeries{}
			for _, label := range series.OtherLabels {
				t.Labels = append(t.Labels, prompb.Label{
					Name:  label.Name,
					Value: label.Value,
				})
			}

			var lastMax int64
			for _, chunk := range series.Chunks {
				// We drop all chunk overlaps, need to work out how to do proper
				// deduplication, see https://github.com/thanos-io/thanos/issues/2700
				// for one idea.
				if len(replicaLabels) > 0 && chunk.MinTime < lastMax {
					continue
				}
				lastMax = chunk.MaxTime

				if chunk.Raw == nil {
					// We only ask for and handle RAW
					err := fmt.Errorf("unexpectedly missing raw chunk data")
					log.Print(err)
					return nil, err
				}
				if chunk.Raw.Type != storepb.Chunk_XOR {
					err := fmt.Errorf("unexpected encoding type: %v", chunk.Raw.Type)
					log.Print(err)
					return nil, err
				}

				raw, err := chunkenc.FromData(chunkenc.EncXOR, chunk.Raw.Data)
				if err != nil {
					err := fmt.Errorf("reading chunk: %w", err)
					log.Print("Error ", err)
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
		}
		response.Results = append(response.Results, result)
	}
	return response, nil
}

// sortChunks combines two list of chunks into chunks in order.
func sortChunks(a, b []storepb.AggrChunk) []storepb.AggrChunk {
	chunks := make([]storepb.AggrChunk, len(a)+len(b))
	log.Printf("chunks: %v %v", len(a), len(b))
	copy(chunks, a)
	copy(chunks[len(a):], b)

	sort.Sort(AggrChunkByTimestamp(chunks))
	return chunks
}

func ok(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("ok"))
}

func root(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-type", "text/html")
	w.Write([]byte(`
	<p>thanos-remote-read adapter</p>
	<ul>
	  <li><a href="/-/healthy">/-/healthy</a>
	  <li><a href="/metrics">/metrics</a>
	  <li>/api/v1/read (point Prometheus here!)
	</ul>`))
}
