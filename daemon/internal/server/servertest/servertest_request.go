// The request and reply the fixture's tests send and read, and the path that serves a request
// through a router as a client on ContractPort does.
package servertest

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strconv"
)

// Do serves one request through the fixture's router, as a client on ContractPort sends it.
func (f *Fixture) Do(req Request) Reply { return ServeRequest(f.Handler, req) }

// ServeRequest serves one request through h, as a client on ContractPort sends it: the Host is the
// contract's authority unless the request's headers name another, a bearer token is set when Token
// is, and a body is sent as JSON unless its Content-Type says otherwise.
func ServeRequest(h http.Handler, req Request) Reply {
	var r *http.Request
	if req.Body != nil {
		r = httptest.NewRequest(req.Method, req.Path, bytes.NewReader(req.Body))
	} else {
		r = httptest.NewRequest(req.Method, req.Path, nil)
	}
	r.Host = "127.0.0.1:" + strconv.Itoa(ContractPort)
	for name, values := range req.Header {
		if name == "Host" && len(values) > 0 {
			r.Host = values[0]
			continue
		}
		r.Header[name] = append([]string(nil), values...)
	}
	if req.Token != "" {
		r.Header.Set("Authorization", "Bearer "+req.Token)
	}
	if req.Body != nil && r.Header.Get("Content-Type") == "" {
		r.Header.Set("Content-Type", "application/json")
	}
	return serve(h, r)
}

// Request is one request the fixture sends: its method, its target (path and query), an optional
// body, extra headers and the bearer token to send (none when empty).
type Request struct {
	Method string
	Path   string
	Body   []byte
	Header http.Header
	Token  string
}

// Reply is a response: its status, its headers and its body.
type Reply struct {
	Status int
	Header http.Header
	Body   []byte
}

// Text is the body as a string.
func (r Reply) Text() string { return string(r.Body) }

// serve runs req through h and reads the recorded response.
func serve(h http.Handler, req *http.Request) Reply {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	res := rec.Result()
	defer func() { _ = res.Body.Close() }() // the response is read; a close error has nothing left to report
	return Reply{Status: res.StatusCode, Header: res.Header, Body: append([]byte(nil), rec.Body.Bytes()...)}
}
