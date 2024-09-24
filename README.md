# oauth2-redirect-controller - Proxy for dynamically swapping OAUTH2 redirect URI

[![release](https://img.shields.io/github/release/DoodleScheduling/oauth2-redirect-controller/all.svg)](https://github.com/DoodleScheduling/oauth2-redirect-controller/releases)
[![release](https://github.com/doodlescheduling/oauth2-redirect-controller/actions/workflows/release.yaml/badge.svg)](https://github.com/doodlescheduling/oauth2-redirect-controller/actions/workflows/release.yaml)
[![report](https://goreportcard.com/badge/github.com/DoodleScheduling/oauth2-redirect-controller)](https://goreportcard.com/report/github.com/DoodleScheduling/oauth2-redirect-controller)
[![OpenSSF Scorecard](https://api.securityscorecards.dev/projects/github.com/DoodleScheduling/oauth2-redirect-controller/badge)](https://api.securityscorecards.dev/projects/github.com/DoodleScheduling/oauth2-redirect-controller)
[![Coverage Status](https://coveralls.io/repos/github/DoodleScheduling/oauth2-redirect-controller/badge.svg?branch=master)](https://coveralls.io/github/DoodleScheduling/oauth2-redirect-controller?branch=master)
[![license](https://img.shields.io/github/license/DoodleScheduling/oauth2-redirect-controller.svg)](https://github.com/DoodleScheduling/oauth2-redirect-controller/blob/master/LICENSE)

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

Please see [chart/oauth2-redirect-controller](https://github.com/DoodleScheduling/oauth2-redirect-controller) for the helm chart docs.

### Manifests/kustomize

Alternatively you may get the bundled manifests in each release to deploy it using kustomize or use them directly.


## Configuration
The controller can be configured using cmd args:
```
--concurrent int                            The number of concurrent reconciles. (default 4)
--enable-leader-election                    Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager.
--graceful-shutdown-timeout duration        The duration given to the reconciler to finish before forcibly stopping. (default 10m0s)
--health-addr string                        The address the health endpoint binds to. (default ":9557")
--insecure-kubeconfig-exec                  Allow use of the user.exec section in kubeconfigs provided for remote apply.
--insecure-kubeconfig-tls                   Allow that kubeconfigs provided for remote apply can disable TLS verification.
--kube-api-burst int                        The maximum burst queries-per-second of requests sent to the Kubernetes API. (default 300)
--kube-api-qps float32                      The maximum queries-per-second of requests sent to the Kubernetes API. (default 50)
--leader-election-lease-duration duration   Interval at which non-leader candidates will wait to force acquire leadership (duration string). (default 35s)
--leader-election-release-on-cancel         Defines if the leader should step down voluntarily on controller manager shutdown. (default true)
--leader-election-renew-deadline duration   Duration that the leading controller manager will retry refreshing leadership before giving up (duration string). (default 30s)
--leader-election-retry-period duration     Duration the LeaderElector clients should wait between tries of actions (duration string). (default 5s)
--log-encoding string                       Log encoding format. Can be 'json' or 'console'. (default "json")
--log-level string                          Log verbosity level. Can be one of 'trace', 'debug', 'info', 'error'. (default "info")
--max-retry-delay duration                  The maximum amount of time for which an object being reconciled will have to wait before a retry. (default 15m0s)
--metrics-addr string                       The address the metric endpoint binds to. (default ":9556")
--min-retry-delay duration                  The minimum amount of time for which an object being reconciled will have to wait before a retry. (default 750ms)
--watch-all-namespaces                      Watch for resources in all namespaces, if set to false it will only watch the runtime namespace. (default true)
--watch-label-selector string               Watch for resources with matching labels e.g. 'sharding.fluxcd.io/shard=shard1'.
```
