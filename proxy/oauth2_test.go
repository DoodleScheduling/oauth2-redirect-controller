package proxy

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestRegisterOrUpdateBackend(t *testing.T) {
	g := NewWithT(t)
	proxy := New(logr.Discard(), &http.Client{})

	path := OAUTH2Proxy{
		Host:        "foo",
		Service:     "bar",
		RedirectURI: "https://oauth2proxy",
		Paths:       []string{"/"},
		Port:        8080,
		Object: client.ObjectKey{
			Name:      "foo",
			Namespace: "bar",
		},
	}

	err := proxy.RegisterOrUpdate(&path)
	g.Expect(err).NotTo(HaveOccurred(), "could not update backend")
	g.Expect(1).To(Equal(len(proxy.dst)))
	g.Expect(path).To(Equal(*proxy.dst[0]))

	path = OAUTH2Proxy{
		Host:        "foo2",
		Service:     "bar2",
		RedirectURI: "https://oauth2proxy2",
		Paths:       []string{"/"},
		Port:        8080,
		Object: client.ObjectKey{
			Name:      "foo",
			Namespace: "bar",
		},
	}

	err = proxy.RegisterOrUpdate(&path)
	g.Expect(err).NotTo(HaveOccurred(), "could not update backend")
	g.Expect(1).To(Equal(len(proxy.dst)))
	g.Expect(path).To(Equal(*proxy.dst[0]))
}

func TestRemoveBackend(t *testing.T) {
	g := NewWithT(t)
	proxy := New(logr.Discard(), &http.Client{})
	err := proxy.Unregister(client.ObjectKey{
		Name: "does-not-exist",
	})
	g.Expect(err).To(Equal(ErrServiceNotRegistered))

	path := OAUTH2Proxy{
		Host:        "foo",
		Service:     "bar",
		RedirectURI: "https://oauth2proxy",
		Paths:       []string{"/"},
		Port:        8080,
		Object: client.ObjectKey{
			Name:      "foo",
			Namespace: "bar",
		},
	}
	_ = proxy.RegisterOrUpdate(&path)
	err = proxy.Unregister(path.Object)
	g.Expect(err).To(Not(HaveOccurred()))
	g.Expect(0).To(Equal(len(proxy.dst)))
}

func TestRouteRecoverOriginRedirectURI(t *testing.T) {
	g := NewWithT(t)
	proxy := New(logr.Discard(), &http.Client{})

	path := OAUTH2Proxy{
		Host:        "foo",
		Service:     "bar",
		RedirectURI: "https://oauth2proxy",
		Paths:       []string{"/"},
		Port:        8080,
		Object: client.ObjectKey{
			Name:      "foo",
			Namespace: "bar",
		},
	}

	err := proxy.RegisterOrUpdate(&path)
	g.Expect(err).NotTo(HaveOccurred(), "could not update backend")

	tests := []struct {
		name           string
		request        func() *http.Request
		expectHTTPCode int
		expectHeaders  http.Header
	}{
		{
			name: "Return service unavailable if no matching backend was found",
			request: func() *http.Request {
				r, _ := http.NewRequest("GET", "/does-not-exists", nil)
				return r
			},
			expectHTTPCode: http.StatusServiceUnavailable,
		},
		{
			name: "Recovered state is undecodable and ends in 400",
			request: func() *http.Request {
				r, _ := http.NewRequest("GET", "https://oauth2proxy?state=invalid", nil)
				return r
			},
			expectHTTPCode: http.StatusBadRequest,
		},
		{
			name: "Recovered origin redirectURL is undecodable and ends in 400",
			request: func() *http.Request {
				st := state{
					OrigRedirectURI: ":):((#///`",
				}

				b, _ := json.Marshal(st)

				r, _ := http.NewRequest("GET", fmt.Sprintf("https://oauth2proxy?state=%s", b), nil)
				return r
			},
			expectHTTPCode: http.StatusBadRequest,
		},
		{
			name: "Recover origin redirect uri and redirect client ends in 303",
			request: func() *http.Request {
				st := state{
					OrigRedirectURI: "https://my-original-uri",
				}

				b, _ := json.Marshal(st)
				r, _ := http.NewRequest("GET", fmt.Sprintf("https://oauth2proxy?state=%s", b), nil)
				return r
			},
			expectHTTPCode: http.StatusSeeOther,
			expectHeaders: http.Header{
				"Location": []string{"https://my-original-uri"},
			},
		},
		{
			name: "Recover origin redirect uri and redirect client ends in 303 including 3rd party state",
			request: func() *http.Request {
				st := state{
					OrigRedirectURI: "https://my-original-uri",
					OrigState:       "my-state",
				}

				b, _ := json.Marshal(st)
				r, _ := http.NewRequest("GET", fmt.Sprintf("https://oauth2proxy?state=%s", b), nil)
				return r
			},
			expectHTTPCode: http.StatusSeeOther,
			expectHeaders: http.Header{
				"Location": []string{"https://my-original-uri?state=my-state"},
			},
		},
		{
			name: "POST redirect fails because no valid state is in post body",
			request: func() *http.Request {
				st := state{
					OrigRedirectURI: "https://my-original-uri",
					OrigState:       "my-state",
				}

				b, _ := json.Marshal(st)
				r, _ := http.NewRequest("POST", fmt.Sprintf("https://oauth2proxy?state=%s", b), nil)
				return r
			},
			expectHTTPCode: http.StatusBadRequest,
		},
		{
			name: "POST redirect extracts origin state and redirets back to origin including the code and state taken from the post form body",
			request: func() *http.Request {
				st := state{
					OrigRedirectURI: "https://my-original-uri",
					OrigState:       "my-state",
				}

				b, _ := json.Marshal(st)
				vals := url.Values{
					"state": []string{string(b)},
					"code":  []string{"foobar"},
				}.Encode()

				r, _ := http.NewRequest("POST", "https://oauth2proxy", io.NopCloser(strings.NewReader(vals)))
				r.Header.Add("Content-Type", "application/x-www-form-urlencoded")

				return r
			},
			expectHTTPCode: http.StatusSeeOther,
			expectHeaders: http.Header{
				"Location": []string{"https://my-original-uri?code=foobar&state=my-state"},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {

			w := httptest.NewRecorder()
			proxy.ServeHTTP(w, test.request())
			g.Expect(test.expectHTTPCode).To(Equal(w.Code))

			for k, v := range test.expectHeaders {
				g.Expect(v).To(Equal(test.request().Response.Header[k]))
			}
		})
	}
}

type dummyTransport struct {
	transport func(r *http.Request) (*http.Response, error)
}

func (t *dummyTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return t.transport(r)
}

func TestChangeRedirectURI(t *testing.T) {
	g := NewWithT(t)

	path := OAUTH2Proxy{
		Host:        "foo",
		Service:     "bar",
		RedirectURI: "https://oauth2proxy",
		Paths:       []string{"/"},
		Port:        8080,
		Object: client.ObjectKey{
			Name:      "foo",
			Namespace: "bar",
		},
	}

	tests := []struct {
		name           string
		path           func() OAUTH2Proxy
		request        func() *http.Request
		transport      func(r *http.Request) (*http.Response, error)
		expectHTTPCode int
		expectBody     string
		expectHeaders  http.Header
	}{
		{
			name: "Simple http proxy call without outgoing location header",
			path: func() OAUTH2Proxy { return path },
			transport: func(r *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
				}, nil
			},
			request: func() *http.Request {
				r, _ := http.NewRequest("GET", "http://foo/bar", nil)
				return r
			},
			expectHTTPCode: http.StatusOK,
		},
		{
			name: "Proxy request failed with error ends in bad request",
			path: func() OAUTH2Proxy { return path },
			transport: func(r *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
				}, errors.New("error")
			},
			request: func() *http.Request {
				r, _ := http.NewRequest("GET", "http://foo/bar", nil)
				return r
			},
			expectHTTPCode: http.StatusBadRequest,
		},
		{
			name: "Parsing of invalid backend response Location header ends in bad request",
			path: func() OAUTH2Proxy { return path },
			transport: func(r *http.Request) (*http.Response, error) {
				header := http.Header{}
				header.Add("Location", ":):((#///`")

				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     header,
				}, nil
			},
			request: func() *http.Request {
				r, _ := http.NewRequest("GET", "http://foo/bar", nil)
				return r
			},
			expectHTTPCode: http.StatusBadRequest,
		},
		{
			name: "Parsing of invalid backend response Location header ends in bad request",
			path: func() OAUTH2Proxy { return path },
			transport: func(r *http.Request) (*http.Response, error) {
				header := http.Header{}
				header.Add("Location", "https://idp?redirect_uri=:):((#///`")

				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     header,
				}, nil
			},
			request: func() *http.Request {
				r, _ := http.NewRequest("GET", "http://foo/bar", nil)
				return r
			},
			expectHTTPCode: http.StatusBadRequest,
		},
		{
			name: "Parsing of invalid redirectURI ends in internal server error",
			path: func() OAUTH2Proxy {
				p := path
				p.RedirectURI = ":):((#///`"
				return p
			},
			transport: func(r *http.Request) (*http.Response, error) {
				header := http.Header{}
				header.Add("Location", "https://idp")

				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     header,
				}, nil
			},
			request: func() *http.Request {
				r, _ := http.NewRequest("GET", "http://foo/bar", nil)
				return r
			},
			expectHTTPCode: http.StatusInternalServerError,
		},
		{
			name: "Swaps redirect_uri and state in Location header and injects the host from the redirectURI",
			path: func() OAUTH2Proxy { return path },
			transport: func(r *http.Request) (*http.Response, error) {
				header := http.Header{}
				header.Add("Location", "https://idp?redirect_uri=https://idp/auth&state=foobar")
				header.Add("X-1", "foo")
				header.Add("X-1", "bar")

				body := strings.NewReader("foo")

				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     header,
					Body:       io.NopCloser(body),
				}, nil
			},
			request: func() *http.Request {
				r, _ := http.NewRequest("GET", "http://foo/bar", nil)
				return r
			},
			expectHTTPCode: http.StatusOK,
			expectBody:     "foo",
			expectHeaders: http.Header{
				"Location": []string{"https://idp?redirect_uri=https%3A%2F%2Foauth2proxy%2Fauth&state=%7B%22origState%22%3A%22foobar%22%2C%22origRedirectURI%22%3A%22https%3A%2F%2Fidp%2Fauth%22%7D"},
				"X-1":      []string{"foo", "bar"},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			proxy := New(logr.Discard(), &http.Client{
				Transport: &dummyTransport{
					transport: test.transport,
				},
			})

			p := test.path()
			_ = proxy.RegisterOrUpdate(&p)

			w := httptest.NewRecorder()
			proxy.ServeHTTP(w, test.request())
			g.Expect(test.expectHTTPCode).To(Equal(w.Code))

			for k, v := range test.expectHeaders {
				g.Expect(v).To(Equal(test.request().Response.Header[k]))
			}

			if test.expectBody != "" {
				body, _ := io.ReadAll(w.Body)
				g.Expect(test.expectBody).To(Equal(string(body)))
			}
		})
	}
}
