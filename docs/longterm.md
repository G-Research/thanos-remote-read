# Long term storage for a single Prometheus instance

```
+----------------+        +-----------+
|                |        |           |
|                | (write)|  thanos   |                 +---------+
|   Prometheus   +------->+  -sidecar |                 |         |
|                |        |           +---------------->+         |
|                |        +-----------+                 | Object  |
|                |                                      | Store   |
|                |        +-----------+                 |         |
|                |        |           |   +---------+   |         |
|                |        |  thanos   |   |         +<--+         |
|                | (read) |  -remote  |   | thanos  |   +---------+
|                +<-------+  -read    +<--+ -store  |
|                |        |           |   |         |
+----------------+        +-----------+   +---------+
```

## Why?

Using Thanos requires querying via thanos-query and potentially using other
components. The architecture of Thanos is very flexible however and by using
thanos-remote-read it's possible to use Thanos as long term storage for a single
Prometheus instance.

This won't give many benefits of Thanos in particular: deduplication in order to
run multiple replicated Prometheus instances and see a combined query result. It
also may have unexpected behaviour if downsampling is used.

However this can be dropped into an existing Prometheus setup to give longer
term retention than TSDB can cope with, without changing the query behaviour
needed from users (i.e. all the data will still be available to query on the
Prometheus instance).

# Setup

The architecture diagram above gives an overview, the key thing to note is this
only needs a few Thanos components:

* thanos-sidecar: To write to the object storage
* thanos-store: To read from the object storage
* thanos-remote-read: To read from the store and serve the data to Prometheus.

Additionally you may want to configure thanos compactor to run as a cronjob to
clean up data, depending on your needs.

## Configuring Prometheus

Set up Prometheus with an external label, e.g.:

```yaml
global:
  external_labels:
    prometheus: my-instance
```

This will ensure the metrics in the object storage are labelled and keep them
separate from other Prometheus instances. (Note in this setup only the
Prometheus instance that wrote the data can read it, but this allows multiple
instances to write to the same store). It's also entirely possible to add a
thanos-query instance to provide a global view over Prometheus instances.

Configure Prometheus to read from thanos-remote-read:

```yaml
remote_read:
  - url: "http://localhost:10080/api/v1/read"
```

Prometheus will automatically add the external labels when querying the remote
read target, so this is all that's needed.
