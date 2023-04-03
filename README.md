# k8soauth2-proxy-controller - Proxy for dynamically swapping OAUTH2 redirect URI

[![release](https://img.shields.io/github/release/DoodleScheduling/k8soauth2-proxy-controller/all.svg)](https://github.com/DoodleScheduling/k8soauth2-proxy-controller/releases)
[![release](https://github.com/doodlescheduling/k8soauth2-proxy-controller/actions/workflows/release.yaml/badge.svg)](https://github.com/doodlescheduling/k8soauth2-proxy-controller/actions/workflows/release.yaml)
[![report](https://goreportcard.com/badge/github.com/DoodleScheduling/k8soauth2-proxy-controller)](https://goreportcard.com/report/github.com/DoodleScheduling/k8soauth2-proxy-controller)
[![Coverage Status](https://coveralls.io/repos/github/DoodleScheduling/k8soauth2-proxy-controller/badge.svg?branch=master)](https://coveralls.io/github/DoodleScheduling/k8soauth2-proxy-controller?branch=master)
[![license](https://img.shields.io/github/license/DoodleScheduling/k8soauth2-proxy-controller.svg)](https://github.com/DoodleScheduling/k8soauth2-proxy-controller/blob/master/LICENSE)

OAUTH2 Proxy server with kubernetes support.
The proxy is used as MitM between your idp and an external idp. The proxy dynamically replaces the redirect_uri.
This is useful if you have multiple environemnts and one or more external idp like google.
On the external idp only the oauth2 proxy needs to be configured as redirect_uri.
The proxy makes sure to route the oauth2 callbacks correctly back to your original idp.

💡 The proxy is not to be confused with a proxy which adds oidc authentication to your apps. It is rather a proxy do dynamically swap redirect_uri for external IdP in order to maintain a single redirect_uri there.

## Why?
OIDC IdP like google don't provide an API to dynamically configure or change OAUTH2 credentials. Meaning there
is no good way of progamatically add/remove redirect_uri or create ad hock credentials.
With the oauth2 proxy its possible to only have the proxy URL configured as redirect_uri.

The proxy transfers any OAUTH2 state from the original request as well as the redirect_uri in the [state param](https://datatracker.ietf.org/doc/html/rfc6749#section-4.1.1).
While the external IdP redirects back to the oauth2 proxy after a successful authorization the oauth2 proxy unpacks the state and transforms the request back to its original.


## Example OAUTH2Proxy

A `OAUTH2Proxy` binds to a kubernetes service.
The http proxy will route and clone incoming http requests to all OAUTH2Proxy backends matching the host.

```yaml
apiVersion: oauth2.infra.doodle.com/v1beta1
kind: OAUTH2Proxy
metadata:
  name: idp
spec:
  host: my-idp
  paths:
  - /
  redirectURI: https://oauth-proxy
  backend:
    serviceName: backend-idp
    servicePort: http
```

## Setup

The proxy should not be exposed directly to the public. Rather should traffic be routed via an ingress controller
and only paths which are used to redirect to the external idp should be routed via the oauth2 proxy.

### Helm chart

Please see [chart/k8soauth2-proxy-controller](https://github.com/DoodleScheduling/k8soauth2-proxy-controller) for the helm chart docs.

### Manifests/kustomize

Alternatively you may get the bundled manifests in each release to deploy it using kustomize or use them directly.

## Configure the controller

You may change base settings for the controller using env variables (or alternatively command line arguments).
Available env variables:

| Name  | Description | Default |
|-------|-------------| --------|
| `METRICS_ADDR` | The address of the metric endpoint binds to. | `:9556` |
| `PROBE_ADDR` | The address of the probe endpoints binds to. | `:9557` |
| `HTTP_ADDR` | The address of the http proxy. | `:8080` |
| `PROXY_READ_TIMEOUT` | Read timeout to the proxy backend. | `30s` |
| `PROXY_WRITE_TIMEOUT` | Write timeout to the proxy backend. | `30s` |
| `ENABLE_LEADER_ELECTION` | Enable leader election for controller manager. | `false` |
| `LEADER_ELECTION_NAMESPACE` | Change the leader election namespace. This is by default the same where the controller is deployed. | `` |
| `NAMESPACES` | The controller listens by default for all namespaces. This may be limited to a comma delimted list of dedicated namespaces. | `` |
| `CONCURRENT` | The number of concurrent reconcile workers.  | `2` |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | The gRPC opentelemtry-collector endpoint uri | `` |

**Note:** The proxy implements opentelemetry tracing, see [further possible env](https://opentelemetry.io/docs/reference/specification/sdk-environment-variables/) variables to configure it.
