# Thanos remote read adapter

This allows querying a Thanos StoreAPI server (store, query, etc.) from Prometheus.

## Why?

Thanos ruler can be unreliable ([see
docs](https://thanos.io/components/rule.md/#risk)). This provides a way to
bring some data from Thanos into a Prometheus instance.

While that may not seem to solve the problem of Thanos query reliability (it is
still querying Thanos), it provides a way to write rules that make the failure
cases more explicit.

### Example

A system has some data in Thanos that is aggregated in a way that isn't
available to the Prometheus instances. Using two rule groups the data can be
pulled into a recording rule in Prometheus, then alerts can be evaluated on the
data, without having to consider how Thanos query failures may manifest on every
query (the queries that depend on a remote system are marked with
`source="thanos"`).

```yaml
# Prometheus recording rules

groups:
  # This group reads from Thanos. Because it depends on a remote system
  # evaluting rules could fail.
  - name: read_thanos
    rules:
    - record: thanos:metric
      expr: thanos_metric{source="thanos"}

  # The alerts can handle absent data how they like, rather than in
  # thanos-ruler, where `partial_response_strategy: ABORT` will always fail.
  - name: alerts
    rules:
    - alert: Something
      # Treat this as an example, the point is it handles errors fetching from
      # Thanos.
      expr: up{job="prometheus"} == 0 and
        (thanos:metric == 42 or absent(thanos:metric) == 1)
```

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
    required_matchers:
      source: "thanos"
    read_recent: true
```

In order to get the benefit of avoiding sending every query to Thanos you need
to configure `required_matchers`. To actually ensure you get results this likely
needs to be added as a selector label on the Thanos query instance you point at:

```
thanos query --store=some-store --selector-label='source="thanos"'
```

(If needed you can run a dedicated Thanos query instance that exists to merely
add that label.)

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
