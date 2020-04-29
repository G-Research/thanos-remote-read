# Thanos remote read adapter

This allows querying a Thanos store from Prometheus.

## Why?

Thanos ruler can be unreliable ([see
docs](https://github.com/thanos-io/thanos/blob/master/docs/components/rule.md#risk)).
This provides a way to bring some data from Thanos into a Prometheus instance.

While that may not seem to solve the problem of Thanos query reliability (it is
still querying Thanos), it allows to essentially emulate Prometheus federation
but with Thanos providing the federated data. It also leads to an architecture
that is easier to reason about.

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
      foo: "bar"
    read_recent: true
```

In order to get the benefit of avoiding sending every query to Thanos you need
to configure `required_matchers`. To actually ensure you get results this likely
needs to be added as a selector label on the Thanos query instance you point at:

```
thanos query --store=some-store --selector-label='foo="bar"'
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
