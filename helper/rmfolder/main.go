package main

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/juruen/rmapi/api/sync15"
	"github.com/juruen/rmapi/filetree"
	"github.com/juruen/rmapi/model"
	"github.com/juruen/rmapi/transport"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--self-test" {
		fmt.Println("iserv-rm-folder-helper: ok")
		return
	}
	device := strings.TrimSpace(os.Getenv("ISERV_RM_DEVICE_TOKEN"))
	user := strings.TrimSpace(os.Getenv("ISERV_RM_USER_TOKEN"))
	if device == "" || user == "" {
		fmt.Fprintln(os.Stderr, "missing reMarkable tokens")
		os.Exit(2)
	}
	tokens := model.AuthTokens{DeviceToken: device, UserToken: user}
	httpCtx := transport.CreateHttpClientCtx(tokens)
	httpCtx.Client.Transport = &readRetryTransport{
		base:   http.DefaultTransport,
		delays: []time.Duration{250 * time.Millisecond, 750 * time.Millisecond},
	}
	ctx, err := sync15.CreateCtx(&httpCtx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	seen := map[string]bool{"/": true}
	folders := []string{"/"}
	filetree.WalkTree(ctx.Filetree().Root(), filetree.FileTreeVistor{Visit: func(node *model.Node, _ []string) bool {
		if node == nil || node.IsRoot() || !node.IsDirectory() || node.Id() == filetree.TrashID {
			return filetree.ContinueVisiting
		}
		p, err := ctx.Filetree().NodeToPath(node)
		if err == nil {
			p = "/" + strings.Trim(strings.ReplaceAll(p, "\\", "/"), "/")
			if p != "/trash" && !strings.HasPrefix(p, "/trash/") && !seen[p] {
				seen[p] = true
				folders = append(folders, p)
			}
		}
		return filetree.ContinueVisiting
	}})
	sort.Slice(folders, func(i, j int) bool { return strings.ToLower(folders[i]) < strings.ToLower(folders[j]) })
	for _, p := range folders {
		if p == "/" {
			fmt.Println("/")
		} else {
			fmt.Println(p + "/")
		}
	}
}

type readRetryTransport struct {
	base   http.RoundTripper
	delays []time.Duration
}

func (t *readRetryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	if req.Method != http.MethodGet {
		return base.RoundTrip(req)
	}

	var response *http.Response
	var err error
	for attempt := 0; ; attempt++ {
		attemptReq := req
		if attempt > 0 {
			attemptReq, err = replayRequest(req)
			if err != nil {
				return nil, err
			}
		}
		response, err = base.RoundTrip(attemptReq)
		if !retryableReadResponse(response, err) || attempt >= len(t.delays) {
			return response, err
		}
		if response != nil && response.Body != nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
			_ = response.Body.Close()
		}
		timer := time.NewTimer(t.delays[attempt])
		select {
		case <-req.Context().Done():
			timer.Stop()
			return nil, req.Context().Err()
		case <-timer.C:
		}
	}
}

func replayRequest(req *http.Request) (*http.Request, error) {
	clone := req.Clone(req.Context())
	if req.Body == nil {
		return clone, nil
	}
	if req.GetBody == nil {
		return nil, fmt.Errorf("cannot retry request body")
	}
	body, err := req.GetBody()
	if err != nil {
		return nil, err
	}
	clone.Body = body
	return clone, nil
}

func retryableReadResponse(response *http.Response, err error) bool {
	if err == nil {
		if response == nil {
			return false
		}
		switch response.StatusCode {
		case http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			return true
		default:
			return false
		}
	}
	var networkErr net.Error
	if errors.As(err, &networkErr) && (networkErr.Timeout() || networkErr.Temporary()) {
		return true
	}
	text := strings.ToLower(err.Error())
	for _, marker := range []string{"connection reset by peer", "connection refused", "unexpected eof", "broken pipe"} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}
