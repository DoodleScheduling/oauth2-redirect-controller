package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	ErrServiceNotRegistered = errors.New("service is not registered")
)

// HttpProxy is the main proxy server
type HttpProxy struct {
	dst    []*OAUTH2Proxy
	client *http.Client
	mutex  sync.Mutex
	log    logr.Logger
}

// OAUTH2Proxy defines the serivce which is proxied
type OAUTH2Proxy struct {
	Host        string
	Service     string
	RedirectURI string
	Paths       []string
	Port        int32
	Object      client.ObjectKey
}

// state is the proxied OAUTH2 state
type state struct {
	OrigState       string `json:"origState,omitempty"`
	OrigRedirectURI string `json:"origRedirectURI,omitempty"`
}

// New creates a new instance of HttpProxy
func New(logger logr.Logger, client *http.Client) *HttpProxy {
	return &HttpProxy{
		log:    logger,
		client: client,
	}
}

// Unregister removes a service from the proxy
func (h *HttpProxy) Unregister(obj client.ObjectKey) error {
	for k, v := range h.dst {
		if v.Object == obj {
			h.dst = append(h.dst[:k], h.dst[k+1:]...)
			return nil
		}
	}

	return ErrServiceNotRegistered
}

// RegisterOrUpdate adds a target to the proxy or updates it if it already exists
func (h *HttpProxy) RegisterOrUpdate(dst *OAUTH2Proxy) error {
	h.mutex.Lock()
	defer h.mutex.Unlock()

	for _, v := range h.dst {
		if v.Object == dst.Object {
			h.log.Info("update http backend", "host", dst.Host, "service", dst.Service, "port", dst.Port)
			v.Host = dst.Host
			v.Port = dst.Port
			v.Service = dst.Service
			v.RedirectURI = dst.RedirectURI
			v.Paths = dst.Paths

			return nil
		}
	}

	h.log.Info("register http backend", "host", dst.Host, "service", dst.Service, "port", dst.Port)
	h.dst = append(h.dst, dst)

	return nil
}

func (h *HttpProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.log.Info("attempt to proxy incoming http request", "request", r.RequestURI, "host", r.Host)

	for _, dst := range h.dst {
		u, err := url.Parse(dst.RedirectURI)
		if err != nil {
			h.log.Info("could not parse proxy redirectURI", "request", r.RequestURI, "host", dst.Host, "err", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		//request targets service, check if response has a redirect uri and state and attempt to change it to the proxy redirectURI
		if dst.Host == r.Host {
			h.changeRedirectURI(w, r, dst)
			return
		}

		//request targets redirectURI, attempt to parse state and redirect to original URL
		if u.Host == r.Host {
			h.recoverIncomingState(w, r, dst)
			return
		}
	}

	// We don't have any matching OAUTH2Proxy resources matching the host
	w.WriteHeader(http.StatusServiceUnavailable)
}

// proxy request to target
// if the request matches a path and the response contains a location header, the proxy
// attempts to change the redirect_url in the location uri to the configured proxy target
func (h *HttpProxy) changeRedirectURI(w http.ResponseWriter, r *http.Request, dst *OAUTH2Proxy) error {
	h.log.Info("found matching http backend for request", "request", r.RequestURI, "host", dst.Host, "service", dst.Service, "port", dst.Port)

	clone := r.Clone(context.TODO())
	clone.URL.Scheme = "http"
	clone.URL.Host = fmt.Sprintf("%s:%d", dst.Service, dst.Port)
	clone.RequestURI = ""

	// send request to proxy target
	res, err := h.client.Do(clone)

	if err != nil {
		h.log.Info("forwarding request to svc backend failed", "err", err, "request", r.RequestURI, "host", dst.Host, "service", dst.Service, "port", dst.Port)
		w.WriteHeader(http.StatusBadRequest)
		return err
	}

	h.log.Info("forwarding request to svc backend finished", "status", res.StatusCode, "host", dst.Host, "service", dst.Service, "port", dst.Port)

	if location, ok := res.Header["Location"]; ok && matchPath(r.URL.Path, dst.Paths) {
		u, err := url.Parse(location[0])
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return err
		}

		vals := u.Query()
		if vals.Get("redirect_uri") != "" {
			st := state{
				OrigState:       vals.Get("state"),
				OrigRedirectURI: vals.Get("redirect_uri"),
			}
			b, _ := json.Marshal(st)

			origRedirectUri, err := url.Parse(vals.Get("redirect_uri"))
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return err
			}

			redirectUri, err := url.Parse(dst.RedirectURI)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				return err
			}
			redirectUri.Path = origRedirectUri.Path

			vals.Set("state", string(b))
			vals.Set("redirect_uri", redirectUri.String())
			u.RawQuery = vals.Encode()

			res.Header["Location"] = []string{u.String()}
		}
	}

	for k, v := range res.Header {
		for _, h := range v {
			w.Header().Add(k, h)
		}
	}

	w.WriteHeader(res.StatusCode)

	io.Copy(w, res.Body)
	res.Body.Close()

	return nil
}

func matchPath(p string, list []string) bool {
	for _, v := range list {
		if strings.HasPrefix(p, v) {
			return true
		}
	}

	return false
}

// recoverIncomingState attempts to parse the incoming state (if there is any) and redirect the request back to the original redirect_uri
func (h *HttpProxy) recoverIncomingState(w http.ResponseWriter, r *http.Request, dst *OAUTH2Proxy) error {
	vals := r.URL.Query()
	str := vals.Get("state")
	state := &state{}

	h.log.Info("request matches redirectURL, attempt to recover state", "host", r.Host, "state", str)

	err := json.Unmarshal([]byte(str), state)
	if err != nil {
		h.log.Info("contains undecodable state", "request", r.RequestURI, "host", dst.Host, "err", err)
		w.WriteHeader(http.StatusBadRequest)
		return err
	}

	u, err := url.Parse(state.OrigRedirectURI)
	if err != nil {
		h.log.Info("could not decode original redirect uri", "request", r.RequestURI, "host", dst.Host, "origRedirectURI", state.OrigRedirectURI, "err", err)
		w.WriteHeader(http.StatusBadRequest)
		return err
	}

	r.URL.Path = u.Path
	r.URL.Host = u.Host

	if state.OrigState != "" {
		vals.Set("state", state.OrigState)
	} else {
		vals.Del("state")
	}

	r.URL.RawQuery = vals.Encode()

	h.log.Info("recovered original state and modified path", "url", r.URL.String(), "host", dst.Host, "path", u.Path, "state", state.OrigState)

	w.Header().Set("Location", r.URL.String())
	w.WriteHeader(http.StatusSeeOther)

	return nil
}
