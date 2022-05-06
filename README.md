# k8soauth2-controller

A cloud native http proxy with the ability to clone incoming requests to multiple backend
services.

![graph](https://github.com/DoodleScheduling/k8soauth2-controller/blob/master/docs/graph.jpg?raw=true)

## Example OAUTH2Proxy

A `OAUTH2Proxy` binds to a kubernetes service.
The http proxy will route and clone incoming http requests to all OAUTH2Proxy backends matching the host.

```yaml
apiVersion: oauth2.infra.doodle.com/v1beta1
kind: OAUTH2Proxy
metadata:
  name: svc-billing-chargebee-webhook
  namespace: default
spec:
  host: chargebee-webhook.kubernetes.doodle-test.com
  backend:
    serviceName: svc-billing
    servicePort: http
```

## Helm chart

Please see [chart/k8soauth2-controller](https://github.com/DoodleScheduling/k8soauth2-controller) for the helm chart docs.

## Configure the controller

You may change base settings for the controller using env variables (or alternatively command line arguments).
Available env variables:

| Name  | Description | Default |
|-------|-------------| --------|
| `METRICS_ADDR` | The address of the metric endpoint binds to. | `:9556` |
| `PROBE_ADDR` | The address of the probe endpoints binds to. | `:9557` |
| `HTTP_ADDR` | The address of the http proxy. | `:8080` |
| `ENABLE_LEADER_ELECTION` | Enable leader election for controller manager. | `false` |
| `LEADER_ELECTION_NAMESPACE` | Change the leader election namespace. This is by default the same where the controller is deployed. | `` |
| `NAMESPACES` | The controller listens by default for all namespaces. This may be limited to a comma delimted list of dedicated namespaces. | `` |
| `CONCURRENT` | The number of concurrent reconcile workers.  | `4` |
