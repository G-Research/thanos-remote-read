<img src="docs/logo/svg/Thanos-remote-read_SignatureLogo_RGB-Black.svg" width="180" alt="thanos-remote-read">

[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)
[![Docker Image Version (latest semver)](https://img.shields.io/docker/v/gresearchdev/thanos-remote-read)](https://hub.docker.com/r/gresearchdev/thanos-remote-read)

This is an adapter that allows querying a [Thanos](https://thanos.io) StoreAPI server
(store, query, etc.) from [Prometheus](https://prometheus.io) via Prometheus's remote
read support.

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

Use the [Docker image](https://hub.docker.com/r/gresearchdev/thanos-remote-read).

Or build latest master:

```
go install github.com/G-Research/thanos-remote-read@latest
```

This will give you `$(go env GOPATH)/bin/thanos-remote-read`

## Running

This is a very simple proxy, so it accepts options of where to listen and where
to forward to:

```
Usage of ./thanos-remote-read:
  -listen string
        [ip]:port to serve HTTP on (default ":10080")
  -store string
        Thanos Store API gRPC endpoint (default "localhost:10901")
  -ignore-warnings
        Ignore warnings from Thanos (default false)
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

### Ignoring certain selectors

In some situations you may have selectors that are present as external labels
on your Prometheus instance (e.g. `prometheus`, `prometheus_replica`, `cluster`
or similar).

These will be passed to the remote read query. In general this is what you want
for using Thanos as a way to make longer term storage visible to the Prometheus
instance the data originated from. However in some cases you may have data
aggregated in different ways in Thanos.

To remove specific labels from the query that is sent to Thanos do something
like:

```yaml
  - url: "http://localhost:10080/api/v1/read?ignore=prometheus_replica"
```

Note that this will not do de-duplication across these labels, so you will need
to make sure if needed to either aggregate in Prometheus (e.g. something like
`max(...) without(prometheus_replica)`), or Thanos (via ruler).

### Warning handling

Loading data from remote read may or may not be considered an error for the
whole query, depending on your exact use case. The default is to propagate the
error to Prometheus, as then you receive a warning when querying. This can be
changed with the `-ignore-warnings` option.

See also https://www.robustperception.io/remote-read-and-partial-failures

## Contributing

We welcome new contributors! We'll happily receive PRs for bug fixes or small
changes. If you're contemplating something larger please get in touch first by
opening a GitHub Issue describing the problem and how you propose to solve it.

## Security

Please see our [security policy](https://github.com/G-Research/thanos-remote-read/blob/master/SECURITY.md) for details on reporting security vulnerabilities.

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
