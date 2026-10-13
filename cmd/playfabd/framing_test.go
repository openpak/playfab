package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"testing"
)

func TestJSONFramingThroughStreamingProxy(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		success(w, map[string]string{"SessionTicket": strings.Repeat("x", 4096)})
	}))
	defer upstream.Close()
	target, _ := url.Parse(upstream.URL)
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.FlushInterval = -1 // same streaming mode as nx-baas
	public := httptest.NewServer(proxy)
	defer public.Close()
	response, err := http.Get(public.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 200 || response.ContentLength != int64(len(body)) || len(response.TransferEncoding) != 0 {
		t.Fatalf("status=%d length=%d actual=%d transfer=%v", response.StatusCode, response.ContentLength, len(body), response.TransferEncoding)
	}
	if !strings.HasSuffix(string(body), "}\n") {
		t.Fatal("incomplete response")
	}
}
