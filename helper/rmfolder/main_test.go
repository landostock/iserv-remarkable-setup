package main

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestReadRetryTransportRecoversFromResetAndServiceFailure(t *testing.T) {
	calls := 0
	transport := &readRetryTransport{
		base: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			calls++
			switch calls {
			case 1:
				return nil, errors.New("read: connection reset by peer")
			case 2:
				return &http.Response{
					StatusCode: http.StatusServiceUnavailable,
					Body:       io.NopCloser(strings.NewReader("retry later")),
					Request:    req,
				}, nil
			default:
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader("ok")),
					Request:    req,
				}, nil
			}
		}),
		delays: []time.Duration{0, 0},
	}
	req, err := http.NewRequest(http.MethodGet, "https://example.test/blob", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || calls != 3 {
		t.Fatalf("status=%d calls=%d", response.StatusCode, calls)
	}
}

func TestReadRetryTransportDoesNotRetryPermanentOrWritingRequests(t *testing.T) {
	for _, test := range []struct {
		name   string
		method string
		status int
	}{
		{name: "permanent read failure", method: http.MethodGet, status: http.StatusUnauthorized},
		{name: "writing request", method: http.MethodPut, status: http.StatusServiceUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			transport := &readRetryTransport{
				base: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					calls++
					return &http.Response{
						StatusCode: test.status,
						Body:       io.NopCloser(strings.NewReader("failure")),
						Request:    req,
					}, nil
				}),
				delays: []time.Duration{0, 0},
			}
			req, err := http.NewRequest(test.method, "https://example.test/blob", strings.NewReader("body"))
			if err != nil {
				t.Fatal(err)
			}
			response, err := transport.RoundTrip(req)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != test.status || calls != 1 {
				t.Fatalf("status=%d calls=%d", response.StatusCode, calls)
			}
		})
	}
}
