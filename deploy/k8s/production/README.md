# Multi-server production topology

This overlay renders only LinkGo workloads. It deliberately does not deploy the
single-node Redis, MySQL, Kafka and Etcd manifests used by the local demo.

1. Replace every `example.internal` endpoint in `configmap.yaml` and create the
   `linkgo-im-secret` Secret from `secret.example.yaml` using a secret manager.
2. Ensure every Logic pod can register its pod IP in the shared Etcd cluster.
3. Render with `kubectl kustomize deploy/k8s/production --load-restrictor LoadRestrictionsNone`.
4. Apply the rendered file, then run the normal immutable-image release script.

`LOGIC_ADDR` must remain unset. A Gateway then watches `/services/logic` in Etcd
and balances gRPC calls across live Logic pod IPs with `p2c_ewma`. Redis is
addressed through one stable HA endpoint (managed Redis or Sentinel-aware
proxy/VIP). MySQL writes use the primary/proxy endpoint in `DB_DSN`; replication
and failover stay behind that endpoint. This code does not claim native Redis
Cluster sharding or application-level MySQL read/write splitting.
