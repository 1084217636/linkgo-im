// secure-demo is a local HTTPS/WSS edge for exercising the existing Gateway.
// It deliberately listens on loopback only; production should use a managed
// TLS ingress and a certificate trusted by real clients.
package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"
)

func newHandler(publicDir, gatewayURL string) (http.Handler, error) {
	upstream, err := url.Parse(gatewayURL)
	if err != nil || upstream.Scheme != "http" || upstream.Host == "" || !isLoopback(upstream.Hostname()) {
		return nil, fmt.Errorf("gateway must be a loopback HTTP URL")
	}
	proxy := httputil.NewSingleHostReverseProxy(upstream)
	proxy.Director = nil
	proxy.Rewrite = func(request *httputil.ProxyRequest) {
		request.SetURL(upstream)
		request.SetXForwarded()
		request.Out.Header.Set("X-Forwarded-Proto", "https")
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		log.Printf("gateway unavailable: %v", err)
		http.Error(w, "Gateway temporarily unavailable", http.StatusBadGateway)
	}
	files := http.FileServer(http.Dir(publicDir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		if r.URL.Path == "/ws" && r.URL.Query().Get("ticket") == "" {
			http.Error(w, "WebSocket ticket required", http.StatusUnauthorized)
			return
		}
		if r.URL.Path == "/ws" || r.URL.Path == "/readyz" || r.URL.Path == "/healthz" || len(r.URL.Path) >= 8 && r.URL.Path[:8] == "/api/v1/" {
			proxy.ServeHTTP(w, r)
			return
		}
		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			w.Header().Set("Cache-Control", "no-store")
		}
		files.ServeHTTP(w, r)
	}), nil
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func main() {
	listen := flag.String("listen", "127.0.0.1:8443", "loopback TLS listen address")
	publicDir := flag.String("public", "public", "static client directory")
	gateway := flag.String("gateway", "http://127.0.0.1:8090", "local Gateway HTTP endpoint")
	cert := flag.String("cert", ".local-tls/cert.pem", "TLS certificate")
	key := flag.String("key", ".local-tls/key.pem", "TLS private key")
	flag.Parse()
	host, _, err := net.SplitHostPort(*listen)
	if err != nil || !isLoopback(host) {
		log.Fatal("secure demo must listen on loopback")
	}
	handler, err := newHandler(*publicDir, *gateway)
	if err != nil {
		log.Fatal(err)
	}
	server := &http.Server{
		Addr:              *listen,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		TLSConfig:         &tls.Config{MinVersion: tls.VersionTLS12},
	}
	log.Printf("secure local demo: https://%s/ -> %s", *listen, *gateway)
	log.Fatal(server.ListenAndServeTLS(*cert, *key))
}
