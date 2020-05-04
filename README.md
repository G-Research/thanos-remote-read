# Thanos remote read adapter

This allows querying a Thanos StoreAPI server (store, query, etc.) from Prometheus.

## Use cases

* [Long term storage](docs/longterm.md): Using Thanos as long term storage for a
  Prometheus instance, in a very transparent way (no need for users to even see
  thanos-query). This could be part of a migration strategy to fully use Thanos
  for example.
* ["ruler"](docs/ruler.md): Running rules outside of Thanos, to provide more
  predictable query reliability than thanos-ruler can offer.
* With [Geras](https://github.com/G-Research/geras): Use Geras to serve OpenTSDB
  data on Thanos StoreAPI, then make that data available to Prometheus.

## Building

```
go build .
```

## Running

This is a very simple proxy, so it accepts options of where to listen and where
to forward to:

```
Usage of ./thanos-remote-read:
  -listen string
        [ip]:port to serve HTTP on (default ":10080")
  -store string
        Thanos Store API gRPC endpoint (default "localhost:10901")
```

For example:
```
./thanos-remote-read -store localhost:10901
```

See the use cases for more background, `-store` can be pointed to anything
implementing Thanos StoreAPI, e.g. `thanos store`, `thanos query`, etc.

## Configuring Prometheus

```yaml
remote_read:
  - url: "http://localhost:10080/api/v1/read"
    # Potentially other options, see use cases docs and 
    # https://prometheus.io/docs/prometheus/latest/configuration/configuration/#remote_read
```

## License

Copyright 2020 G-Research

Licensed under the Apache License, Version 2.0 (the "License"); you may not use
these files except in compliance with the License. You may obtain a copy of the
License at

http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed
under the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR
CONDITIONS OF ANY KIND, either express or implied. See the License for the
specific language governing permissions and limitations under the License.
