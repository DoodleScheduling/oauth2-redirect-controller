package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	ErrServiceNotRegistered = errors.New("service is not registered")
)

type OAUTH2Proxy struct {
	Host        string
	RedirectURI string
	Service     string
	Port        int32
	Object      client.ObjectKey
}

type HttpProxy struct {
	dst    []*OAUTH2Proxy
	client *http.Client
	mutex  sync.Mutex
	log    logr.Logger
	opts   HttpProxyOptions
}

type HttpProxyOptions struct {
	BodySizeLimit int64
}

func New(logger logr.Logger, client *http.Client, opts HttpProxyOptions) *HttpProxy {
	return &HttpProxy{
		log:    logger,
		client: client,
		opts:   opts,
	}
}

func (h *HttpProxy) Unregister(obj client.ObjectKey) error {
	for k, v := range h.dst {
		if v.Object == obj {
			h.dst = append(h.dst[:k], h.dst[k+1:]...)
			return nil
		}
	}

	return ErrServiceNotRegistered
}

func (h *HttpProxy) RegisterOrUpdate(dst *OAUTH2Proxy) error {
	h.mutex.Lock()
	defer h.mutex.Unlock()

	for _, v := range h.dst {
		if v.Object == dst.Object {
			v.Host = dst.Host
			v.Port = dst.Port
			v.Service = dst.Service

			return nil
		}
	}

	h.log.Info("register new http backend", "host", dst.Host, "service", dst.Service, "port", dst.Port)
	h.dst = append(h.dst, dst)

	return nil
}

type state struct {
	OrigState       string `json:"origState,omitempty"`
	OrigRedirectURI string `json:"origRedirectURI,omitempty"`
}

func (h *HttpProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.log.Info("attempt to proxy incoming http request", "request", r.RequestURI, "host", r.Host)
	var found bool

	for _, dst := range h.dst {
		u, err := url.Parse(dst.RedirectURI)
		if err != nil {
			continue
		}

		//request targets redirectURI, attempt to parse state and redirect to original URL
		if u.Host == r.Host {
			vals := r.URL.Query()
			str := vals.Get("state")
			state := &state{}
			h.log.Info("request matches redirectURL, attempt to recover state", "host", r.Host, "state", str)

			err := json.Unmarshal([]byte(str), state)
			if err != nil {
				h.log.Info("contains undecodable state", "request", r.RequestURI, "host", dst.Host, "err", err)
				continue
			}

			u, err := url.Parse(state.OrigRedirectURI)
			if err != nil {
				h.log.Info("could not decode original redirect uri", "request", r.RequestURI, "host", dst.Host, "origRedirectURI", state.OrigRedirectURI, "err", err)
				continue
			}

			r.URL.Path = u.Path
			r.URL.Host = u.Host
			vals.Set("state", state.OrigState)
			r.URL.RawQuery = vals.Encode()

			h.log.Info("recovered original state and modified path", "url", r.URL.String(), "host", dst.Host, "path", u.Path, "state", state.OrigState)

			w.Header().Set("Location", r.URL.String())
			w.WriteHeader(http.StatusSeeOther)
			break
		}

		//request targets service, check if response has a redirect uri and state and attempt to change it to the proxy redirectURI
		if dst.Host == r.Host {
			h.log.Info("found matching http backend for request", "request", r.RequestURI, "host", dst.Host, "service", dst.Service, "port", dst.Port)
			found = true

			clone := r.Clone(context.TODO())
			clone.URL.Scheme = "http"
			clone.URL.Host = fmt.Sprintf("%s:%d", dst.Service, dst.Port)
			clone.RequestURI = ""

			res, err := h.client.Do(clone)
			if err != nil {
				h.log.Error(err, "forwarding request to svc backend failed", "request", r.RequestURI, "host", dst.Host, "service", dst.Service, "port", dst.Port)
			} else {
				h.log.Info("forwarding request to svc backend finished", "status", res.StatusCode, "host", dst.Host, "service", dst.Service, "port", dst.Port)
			}

			if location, ok := res.Header["Location"]; ok {
				u, err := url.Parse(location[0])
				if err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					return
				}

				vals := u.Query()
				if vals.Get("state") != "" && vals.Get("redirect_uri") != "" {
					st := state{
						OrigState:       vals.Get("state"),
						OrigRedirectURI: vals.Get("redirect_uri"),
					}
					b, _ := json.Marshal(st)

					vals.Set("state", string(b))
					vals.Set("redirect_uri", dst.RedirectURI)
					u.RawQuery = vals.Encode()

					res.Header["Location"] = []string{u.String()}
				}
			}

			for k, v := range res.Header {
				w.Header().Set(k, v[0])
			}

			w.WriteHeader(res.StatusCode)

			io.Copy(w, res.Body)
			res.Body.Close()

			break
		}
	}

	// We don't have any matching OAUTH2Proxy resources matching the host
	if found == false {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
}
