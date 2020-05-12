package main

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gogo/protobuf/proto"
	"github.com/golang/snappy"
	"github.com/prometheus/prometheus/prompb"
	"github.com/thanos-io/thanos/pkg/store/storepb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/test/bufconn"
)

// Based on code from https://stackoverflow.com/a/52080545
const bufSize = 1024 * 1024

var lis *bufconn.Listener

func init() {
	lis = bufconn.Listen(bufSize)
	s := grpc.NewServer()
	storepb.RegisterStoreServer(s, &storepb.UnimplementedStoreServer{})
	go func() {
		if err := s.Serve(lis); err != nil {
			log.Fatalf("Server exited with error: %v", err)
		}
	}()
}

func bufDialer(context.Context, string) (net.Conn, error) {
	return lis.Dial()
}

func TestMain(m *testing.M) {
	var logOutput bytes.Buffer
	log.SetOutput(&logOutput)

	ctx := context.Background()
	conn, err := grpc.DialContext(ctx, "bufnet", grpc.WithContextDialer(bufDialer), grpc.WithInsecure())
	if err != nil {
		log.Fatalf("Failed to dial bufnet: %v", err)
	}

	defer conn.Close()
	setup(conn)

	status := m.Run()
	if status != 0 {
		fmt.Fprint(os.Stderr, logOutput.String())
	}
	os.Exit(status)
}

func TestURLs(t *testing.T) {
	for i, h := range []struct {
		url            string
		expectedCode   int
		expectedString string
	}{
		{"/", http.StatusOK, "thanos-remote-read"},
		{"/metrics", http.StatusOK, "# HELP "},
		{"/-/healthy", http.StatusOK, "ok"},
		// No body provided, so bad request
		{"/api/v1/read", http.StatusBadRequest, ""},
		{"/404", http.StatusNotFound, ""},
	} {
		r := httptest.NewRequest("GET", h.url, nil)
		w := httptest.NewRecorder()
		http.DefaultServeMux.ServeHTTP(w, r)
		if w.Code != h.expectedCode {
			t.Errorf("%d: got %v, expected %v", i, w.Code, h.expectedCode)
		}
		body := w.Body.String()
		if len(h.expectedString) != 0 && !strings.Contains(body, h.expectedString) {
			t.Errorf("%d: got %q, expected to contain %q", i, body, h.expectedString)
		}
	}
}

func TestReadEmpty(t *testing.T) {
	// An empty request, results in no queries! => OK
	request := &prompb.ReadRequest{}
	rbuf, err := proto.Marshal(request)
	if err != nil {
		t.Errorf("proto marshal: %v", err)
	}
	sbuf := snappy.Encode(nil, rbuf)
	r := httptest.NewRequest("POST", "/api/v1/read", bytes.NewReader(sbuf))
	w := httptest.NewRecorder()
	http.DefaultServeMux.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("got %v, expected %v", w.Code, http.StatusOK)
	}
}

func TestReadBasic(t *testing.T) {
	// A simple query => error because we use UnimplementedStoreServer.
	request := &prompb.ReadRequest{
		Queries: []*prompb.Query{
			{
				Matchers: []*prompb.LabelMatcher{
					{Name: "__name__", Value: "test"},
				},
			},
		},
	}
	rbuf, err := proto.Marshal(request)
	if err != nil {
		t.Errorf("proto marshal: %v", err)
	}
	sbuf := snappy.Encode(nil, rbuf)
	r := httptest.NewRequest("POST", "/api/v1/read", bytes.NewReader(sbuf))
	w := httptest.NewRecorder()
	http.DefaultServeMux.ServeHTTP(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("got %v, expected %v", w.Code, http.StatusInternalServerError)
	}
}
