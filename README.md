# Thanos remote read adapter

This allows querying a Thanos StoreAPI server (store, query, etc.) from Prometheus.

## Use cases

* [Long term storage](docs/longterm.md): Using Thanos as long term storage for a
  Prometheus instance, in a very transparent way (no need for users to even see
  thanos-query). This could be part of a migration strategy to fully use Thanos
  for example.
* ["ruler"](docs/ruler.md): Running rules outside of Thanos, to provide more
  predictable query reliability than thanos-ruler can offer.

## Building

```
go build .
```

## Running

```
./thanos-remote-read
```

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
