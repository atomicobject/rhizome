package web

import (
	"io"
	"net/http"
	"net/http/httptest"
)

func newApplicationRequest(method, target string, body io.Reader) *http.Request {
	request := httptest.NewRequest(method, target, body)
	request.Host = "localhost"
	request.RemoteAddr = "127.0.0.1:1234"
	return request
}
