module github.com/G-Research/thanos-remote-read

go 1.13

require (
	cloud.google.com/go v0.74.0 // indirect
	github.com/gogo/protobuf v1.3.1
	github.com/golang/snappy v0.0.1
	github.com/grpc-ecosystem/go-grpc-prometheus v1.2.0
	github.com/prometheus/client_golang v1.5.1
	github.com/prometheus/prometheus v1.8.2-0.20200428100226-05038b48bdf0
	github.com/thanos-io/thanos v0.12.1
	go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc v0.16.0
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.16.0
	go.opentelemetry.io/otel v0.16.0
	go.opentelemetry.io/otel/exporters/trace/jaeger v0.16.0
	golang.org/x/sys v0.0.0-20210104204734-6f8348627aad // indirect
	google.golang.org/genproto v0.0.0-20201214200347-8c77b98c765d // indirect
	google.golang.org/grpc v1.34.0
)
