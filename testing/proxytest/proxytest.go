// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package proxytest

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
)

type Proxy struct {
	*httptest.Server

	// Port is the port Server is listening on.
	Port string

	// LocalhostURL is the server URL as "http://localhost:PORT".
	LocalhostURL string

	// proxiedRequests is a "request log" for every request the proxy receives.
	proxiedRequests   []string
	proxiedRequestsMu sync.Mutex

	opts options
}

type Option func(o *options)

type loggingF func(format string, a ...any)

type options struct {
	addr        string
	rewriteHost func(string) string
	rewriteURL  func(u *url.URL)
	// logFn if set will be used to log every request.
	logFn           loggingF
	verbose         bool
	serverTLSConfig *tls.Config
	client          *http.Client
}

// WithAddress will set the address the server will listen on. The format is as
// defined by net.Listen for a tcp connection.
func WithAddress(addr string) Option {
	return func(o *options) {
		o.addr = addr
	}
}

// WithRequestLog sets the proxy to log every request using logFn. It uses name
// as a prefix to the log.
func WithRequestLog(name string, logFn func(format string, a ...any)) Option {
	return func(o *options) {
		o.logFn = func(format string, a ...any) {
			logFn("[proxy-"+name+"] "+format, a...)
		}
	}
}

// WithRewrite will replace old by new on the request URL host when forwarding it.
func WithRewrite(old, new string) Option {
	return func(o *options) {
		o.rewriteHost = func(s string) string {
			return strings.Replace(s, old, new, 1)
		}
	}
}

// WithVerboseLog sets the proxy to log every request verbosely. WithRequestLog
// must be used as well, otherwise WithVerboseLog will not take effect.
func WithVerboseLog() Option {
	return func(o *options) {
		o.verbose = true
	}
}

// WithRewriteFn calls f on the request *url.URL before forwarding it.
// It takes precedence over WithRewrite. Use if more control over the rewrite
// is needed.
func WithRewriteFn(f func(u *url.URL)) Option {
	return func(o *options) {
		o.rewriteURL = f
	}
}

func WithServerTLSConfig(tc *tls.Config) Option {
	return func(o *options) {
		o.serverTLSConfig = tc
	}
}

func WithHttpClient(hc *http.Client) Option {
	return func(o *options) {
		o.client = hc
	}
}

// New returns a new Proxy ready for use. Use:
//   - WithAddress to set the proxy's address,
//   - WithRewrite or WithRewriteFn to rewrite the URL before forwarding the request.
//
// Check the other With* functions for more options.
func New(t *testing.T, optns ...Option) *Proxy {
	t.Helper()

	opts := options{addr: ":0"}
	for _, o := range optns {
		o(&opts)
	}

	if opts.logFn == nil {
		opts.logFn = func(format string, a ...any) {}
	}

	l, err := net.Listen("tcp", opts.addr) //nolint:gosec,nolintlint // it's a test
	if err != nil {
		t.Fatalf("NewServer failed to create a net.Listener: %v", err)
	}

	p := Proxy{opts: opts}

	p.Server = httptest.NewUnstartedServer(
		http.HandlerFunc(func(ww http.ResponseWriter, r *http.Request) {
			w := &statusResponseWriter{w: ww}

			requestID := uuid.New().String()
			opts.logFn("[%s] STARTING - %s %s %s %s\n",
				requestID, r.Method, r.URL, r.Proto, r.RemoteAddr)

			p.ServeHTTP(w, r)

			opts.logFn(fmt.Sprintf("[%s] DONE %d - %s %s %s %s\n",
				requestID, w.statusCode, r.Method, r.URL, r.Proto, r.RemoteAddr))
		}),
	)
	p.Server.Listener = l

	if opts.serverTLSConfig != nil {
		p.Server.TLS = opts.serverTLSConfig
	}

	u, err := url.Parse(p.URL)
	if err != nil {
		panic(fmt.Sprintf("could parse fleet-server URL: %v", err))
	}

	p.Port = u.Port()
	p.LocalhostURL = "http://localhost:" + p.Port

	opts.logFn("running on %s -> %s", p.URL, p.LocalhostURL)
	return &p
}

func (p *Proxy) Start() error {
	p.Server.Start()
	u, err := url.Parse(p.URL)
	if err != nil {
		return fmt.Errorf("could not parse fleet-server URL: %w", err)
	}

	p.Port = u.Port()
	p.LocalhostURL = "http://localhost:" + p.Port

	p.opts.logFn("running on %s -> %s", p.URL, p.LocalhostURL)
	return nil
}

func (p *Proxy) StartTLS() error {
	p.Server.StartTLS()
	u, err := url.Parse(p.URL)
	if err != nil {
		return fmt.Errorf("could not parse fleet-server URL: %w", err)
	}

	p.Port = u.Port()
	p.LocalhostURL = "http://localhost:" + p.Port

	p.opts.logFn("running on %s -> %s", p.URL, p.LocalhostURL)
	return nil
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if p.opts.verbose {
		requestDump, _ := httputil.DumpRequest(r, true)
		p.opts.logFn("original Request: %s", requestDump)
	}

	origURL := r.URL.String()

	switch {
	case p.opts.rewriteURL != nil:
		p.opts.rewriteURL(r.URL)
	case p.opts.rewriteHost != nil:
		r.URL.Host = p.opts.rewriteHost(r.URL.Host)
	}

	if p.opts.verbose {
		p.opts.logFn("original URL: %s, new URL: %s",
			origURL, r.URL.String())
	}

	p.proxiedRequestsMu.Lock()
	p.proxiedRequests = append(p.proxiedRequests,
		fmt.Sprintf("%s - %s %s %s",
			r.Method, r.URL.Scheme, r.URL.Host, r.URL.String()))
	p.proxiedRequestsMu.Unlock()

	if r.Method == http.MethodConnect {
		p.handleHttps(w, r)
		return
	}

	r.RequestURI = ""

	client := p.opts.client
	if client == nil {
		client = http.DefaultClient
	}

	if p.opts.verbose {
		requestDump, _ := httputil.DumpRequest(r, true)
		p.opts.logFn("modified Request: %s", requestDump)
	}

	modifiedRequest, _ := http.NewRequestWithContext(r.Context(), r.Method, r.URL.String(), r.Body)
	if p.opts.verbose {
		requestDump, _ := httputil.DumpRequest(modifiedRequest, true)
		p.opts.logFn("modified Request #2: %s", requestDump)
	}

	resp, err := client.Do(modifiedRequest)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		msg := fmt.Sprintf("could not make request: %#v", err.Error())
		log.Print(msg)
		_, _ = fmt.Fprint(w, msg)
		return
	}
	defer resp.Body.Close()

	w.WriteHeader(resp.StatusCode)
	for k, v := range resp.Header {
		w.Header()[k] = v
	}

	if _, err = io.Copy(w, resp.Body); err != nil {
		p.opts.logFn("[ERROR] could not write response body: %v", err)
	}
}

func (p *Proxy) handleHttps(w http.ResponseWriter, r *http.Request) {
	targetSiteCon, err := net.Dial("tcp", r.URL.Host)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		msg := fmt.Sprintf("could not connect to %q: %#v", r.URL.Host, err.Error())
		log.Print(msg)
		_, _ = fmt.Fprint(w, msg)
		return
	}

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		w.WriteHeader(http.StatusInternalServerError)
		msg := "httpserver does not support hijacking"
		log.Print(msg)
		_, _ = fmt.Fprint(w, msg)
		return
	}

	proxyClientCon, _, err := hijacker.Hijack()
	_, err = proxyClientCon.Write([]byte("HTTP/1.0 200 Connection established\r\n\r\n"))

	go func() {
		defer proxyClientCon.Close()
		defer targetSiteCon.Close()

		var wg sync.WaitGroup
		wg.Add(2)
		go copyOrWarn(p.opts.logFn, targetSiteCon, proxyClientCon, &wg)
		go copyOrWarn(p.opts.logFn, proxyClientCon, targetSiteCon, &wg)
		wg.Wait()

	}()
}

func copyOrWarn(l loggingF, dst io.Writer, src io.Reader, wg *sync.WaitGroup) {
	if _, err := io.Copy(dst, src); err != nil {
		l("Error copying to client: %s", err)
	}
	wg.Done()
}

// ProxiedRequests returns a slice with the "request log" with every request the
// proxy received.
func (p *Proxy) ProxiedRequests() []string {
	p.proxiedRequestsMu.Lock()
	defer p.proxiedRequestsMu.Unlock()

	var rs []string
	rs = append(rs, p.proxiedRequests...)
	return rs
}

// statusResponseWriter wraps a http.ResponseWriter to expose the status code
// through statusResponseWriter.statusCode
type statusResponseWriter struct {
	w          http.ResponseWriter
	statusCode int
}

func (s *statusResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return s.w.(http.Hijacker).Hijack()
}

func (s *statusResponseWriter) Header() http.Header {
	return s.w.Header()
}

func (s *statusResponseWriter) Write(bs []byte) (int, error) {
	return s.w.Write(bs)
}

func (s *statusResponseWriter) WriteHeader(statusCode int) {
	s.statusCode = statusCode
	s.w.WriteHeader(statusCode)
}

func (s *statusResponseWriter) StatusCode() int {
	return s.statusCode
}
