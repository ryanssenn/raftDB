package main

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

func registerProxyRoutes(mux *http.ServeMux) {
	mux.Handle("/prometheus/", prometheusProxy())
}

func prometheusProxy() http.Handler {
	target, _ := url.Parse("http://localhost:9090")
	proxy := httputil.NewSingleHostReverseProxy(target)
	orig := proxy.Director
	proxy.Director = func(req *http.Request) {
		orig(req)
		req.Host = target.Host
		req.URL.Path = strings.TrimPrefix(req.URL.Path, "/prometheus")
		if req.URL.Path == "" {
			req.URL.Path = "/"
		}
	}
	return proxy
}
