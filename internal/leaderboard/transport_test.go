package leaderboard

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestHTTPClientConfiguresBoundedRequestTimeout(t *testing.T) {
	client, err := NewHTTPClient("")

	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	if client.Timeout <= 0 {
		t.Fatal("factory-created HTTP requests must have a finite timeout")
	}
}

func TestHTTPClientRoutesHTTPSAccordingToProxyConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name, config, host string
		environment        map[string]string
		want               string
	}{
		{"environment by default", "", "example.invalid", map[string]string{"HTTPS_PROXY": "environment"}, "environment"},
		{"direct without environment", "direct", "example.invalid", nil, "direct"},
		{"HTTPS environment wins", "configured", "example.invalid", map[string]string{"HTTPS_PROXY": "environment"}, "environment"},
		{"direct overridden by environment", "direct", "example.invalid", map[string]string{"HTTPS_PROXY": "environment"}, "environment"},
		{"fallback per scheme", "configured", "example.invalid", map[string]string{"HTTP_PROXY": "environment"}, "configured"},
		{"NO_PROXY bypasses config", "configured", "example.invalid", map[string]string{"NO_PROXY": "example.invalid"}, "direct"},
		{"NO_PROXY bypasses environment", "configured", "example.invalid", map[string]string{"HTTPS_PROXY": "environment", "NO_PROXY": "example.invalid"}, "direct"},
		{"NO_PROXY subdomain", "configured", "sub.example.invalid", map[string]string{"NO_PROXY": "example.invalid"}, "direct"},
		{"NO_PROXY CIDR", "configured", "10.20.30.40", map[string]string{"NO_PROXY": "10.0.0.0/8"}, "direct"},
		{"NO_PROXY wildcard", "configured", "example.invalid", map[string]string{"NO_PROXY": "*"}, "direct"},
		{"lowercase environment", "configured", "example.invalid", map[string]string{"https_proxy": "environment"}, "environment"},
		{"localhost bypasses both proxies", "configured", "localhost", map[string]string{"HTTPS_PROXY": "environment"}, "direct"},
		{"IPv4 loopback bypasses both proxies", "configured", "127.0.0.1", map[string]string{"HTTPS_PROXY": "environment"}, "direct"},
		{"IPv6 loopback bypasses both proxies", "configured", "[::1]", map[string]string{"HTTPS_PROXY": "environment"}, "direct"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clearProxyEnvironment(t)
			routes := make(chan string, 4)
			handler := func(name string) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					routes <- name
					if r.Method == http.MethodConnect {
						w.WriteHeader(http.StatusBadGateway)
						return
					}
					_, _ = io.WriteString(w, name)
				})
			}
			configured := httptest.NewServer(handler("configured"))
			defer configured.Close()
			environment := httptest.NewServer(handler("environment"))
			defer environment.Close()
			target := httptest.NewTLSServer(handler("direct"))
			defer target.Close()
			addresses := map[string]string{"configured": configured.URL, "environment": environment.URL, "direct": "direct", "": ""}
			for key, value := range tc.environment {
				t.Setenv(key, strings.NewReplacer("environment", environment.URL).Replace(value))
			}
			client, err := NewHTTPClient(addresses[tc.config])
			if err != nil {
				t.Fatal(err)
			}
			defer client.CloseIdleConnections()
			destination := "https://" + tc.host + "/resource"
			trustFixture(t, client, target.Certificate())
			configureFixtureDestination(t, client, destination, target.Listener.Addr().String())
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			request, _ := http.NewRequestWithContext(ctx, http.MethodGet, destination, nil)

			response, requestErr := client.Do(request)
			if response != nil {
				io.Copy(io.Discard, response.Body)
				response.Body.Close()
			}

			select {
			case route := <-routes:
				if route != tc.want {
					t.Fatalf("route=%s want=%s (request error=%v)", route, tc.want, requestErr)
				}
			default:
				t.Fatalf("no fixture received the request: %v", requestErr)
			}
		})
	}
}

func TestHTTPClientUsesHTTPEnvironmentProxyOverConfiguredProxy(t *testing.T) {
	clearProxyEnvironment(t)
	configured := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "configured") }))
	defer configured.Close()
	environment := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "environment") }))
	defer environment.Close()
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "direct") }))
	defer target.Close()
	t.Setenv("HTTP_PROXY", environment.URL)
	client, err := NewHTTPClient(configured.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	destination := "http://example.invalid/resource"
	configureFixtureDestination(t, client, destination, target.Listener.Addr().String())

	response, requestErr := client.Get(destination)
	if requestErr != nil {
		t.Fatal(requestErr)
	}
	body, readErr := io.ReadAll(response.Body)
	response.Body.Close()

	if readErr != nil || string(body) != "environment" {
		t.Fatalf("HTTP route=%q error=%v; want environment proxy", body, readErr)
	}
}

func TestHTTPClientBypassesConfiguredProxyForLoopback(t *testing.T) {
	clearProxyEnvironment(t)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "proxied") }))
	defer proxy.Close()
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "direct") }))
	defer target.Close()
	client, err := NewHTTPClient(proxy.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()

	response, requestErr := client.Get(target.URL)
	if requestErr != nil {
		t.Fatal(requestErr)
	}
	body, readErr := io.ReadAll(response.Body)
	response.Body.Close()

	if readErr != nil || string(body) != "direct" {
		t.Fatalf("loopback route=%q error=%v; want direct", body, readErr)
	}
}

func TestHTTPClientSupportsAuthenticatedHTTPProxy(t *testing.T) {
	clearProxyEnvironment(t)
	var destination, authorization string
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		destination, authorization = r.URL.String(), r.Header.Get("Proxy-Authorization")
		_, _ = io.WriteString(w, "proxied")
	}))
	defer proxy.Close()
	address, _ := url.Parse(proxy.URL)
	address.User = url.UserPassword("user", "pass")
	client, err := NewHTTPClient(address.String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()

	response, requestErr := client.Get("http://example.invalid/resource")
	if requestErr != nil {
		t.Fatal(requestErr)
	}
	body, readErr := io.ReadAll(response.Body)
	response.Body.Close()

	if readErr != nil || string(body) != "proxied" || destination != "http://example.invalid/resource" || authorization != "Basic dXNlcjpwYXNz" {
		t.Fatalf("authenticated HTTP proxy routing failed: destination=%s auth=%s error=%v", destination, authorization, readErr)
	}
}

func TestHTTPClientSupportsAuthenticatedHTTPSProxy(t *testing.T) {
	clearProxyEnvironment(t)
	var destination, authorization string
	proxy := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		destination, authorization = r.URL.String(), r.Header.Get("Proxy-Authorization")
		_, _ = io.WriteString(w, "proxied")
	}))
	defer proxy.Close()
	address, _ := url.Parse(proxy.URL)
	address.User = url.UserPassword("user", "pass")
	client, err := NewHTTPClient(address.String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	trustFixture(t, client, proxy.Certificate())

	response, requestErr := client.Get("http://example.invalid/resource")
	if requestErr != nil {
		t.Fatal(requestErr)
	}
	body, readErr := io.ReadAll(response.Body)
	response.Body.Close()

	if readErr != nil || string(body) != "proxied" || destination != "http://example.invalid/resource" || authorization != "Basic dXNlcjpwYXNz" {
		t.Fatalf("authenticated HTTPS proxy routing failed: destination=%s auth=%s error=%v", destination, authorization, readErr)
	}
}

func TestHTTPClientSupportsAuthenticatedSOCKSProxies(t *testing.T) {
	for _, scheme := range []string{"socks5", "socks5h"} {
		t.Run(scheme, func(t *testing.T) {
			clearProxyEnvironment(t)
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			captured := make(chan socksRequest, 1)
			done := make(chan struct{})
			go func() {
				defer close(done)
				conn, err := listener.Accept()
				if err != nil {
					return
				}
				serveSOCKSFixture(conn, captured, serveHTTPFixtureResponse)
			}()
			client, err := NewHTTPClient(scheme + "://user:pass@" + listener.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			defer client.CloseIdleConnections()
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			request, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://example.invalid/resource", nil)

			response, requestErr := client.Do(request)
			if requestErr != nil {
				t.Fatal(requestErr)
			}
			body, readErr := io.ReadAll(response.Body)
			response.Body.Close()
			<-done

			if readErr != nil || string(body) != "proxied" {
				t.Fatalf("SOCKS response=%q error=%v", body, readErr)
			}
			select {
			case got := <-captured:
				if got.user != "user" || got.password != "pass" || got.destination != "example.invalid:80" {
					t.Fatalf("SOCKS routing/authentication: %+v", got)
				}
			default:
				t.Fatal("SOCKS request was not observed")
			}
		})
	}
}

func TestHTTPClientRoutesHTTPSViaSOCKSProxy(t *testing.T) {
	for _, tc := range []struct{ name, scheme, httpEnvironment string }{
		{"socks5", "socks5", ""},
		{"socks5h", "socks5h", ""},
		{"HTTPS falls back despite HTTP environment", "socks5h", "http://127.0.0.1:1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clearProxyEnvironment(t)
			t.Setenv("HTTP_PROXY", tc.httpEnvironment)
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			captured := make(chan socksRequest, 1)
			done := make(chan struct{})
			go func() {
				defer close(done)
				conn, err := listener.Accept()
				if err != nil {
					return
				}
				// Close after the handshake: this scenario tests HTTPS routing,
				// not the destination TLS server's response.
				serveSOCKSFixture(conn, captured, func(net.Conn) {})
			}()
			client, err := NewHTTPClient(tc.scheme + "://user:pass@" + listener.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			defer client.CloseIdleConnections()
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			request, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://example.invalid/resource", nil)

			response, requestErr := client.Do(request)
			if response != nil {
				response.Body.Close()
			}
			listener.Close()
			<-done

			if requestErr == nil {
				t.Fatal("fixture deliberately closes before TLS completes")
			}
			select {
			case got := <-captured:
				if got.destination != "example.invalid:443" || got.user != "user" || got.password != "pass" {
					t.Fatalf("HTTPS SOCKS routing/authentication: %+v", got)
				}
			default:
				t.Fatalf("HTTPS did not reach the configured SOCKS proxy: %v", requestErr)
			}
		})
	}
}

func TestHTTPClientRoutesHTTPSViaHTTPSProxy(t *testing.T) {
	clearProxyEnvironment(t)
	connections := make(chan string, 1)
	proxy := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connections <- r.Method + " " + r.Host
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer proxy.Close()
	client, err := NewHTTPClient(proxy.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	trustFixture(t, client, proxy.Certificate())
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://secure.example.invalid/resource", nil)

	response, requestErr := client.Do(request)
	if response != nil {
		response.Body.Close()
	}

	if requestErr == nil {
		t.Fatal("fixture deliberately rejects the tunnel")
	}
	select {
	case connection := <-connections:
		if connection != "CONNECT secure.example.invalid:443" {
			t.Fatalf("unexpected tunnel: %s", connection)
		}
	default:
		t.Fatalf("HTTPS did not reach the TLS proxy: %v", requestErr)
	}
}

func TestHTTPClientRejectsInvalidProxyURLs(t *testing.T) {
	clearProxyEnvironment(t)
	for _, address := range []string{"localhost:8080", "ftp://localhost:8080", "http://", "http://localhost:0", "http://localhost:65536", "http://localhost:", "http://localhost/path", "http://localhost?query=yes", "http://localhost#fragment", "http://user:secret@%zz"} {
		t.Run(address, func(t *testing.T) {
			_, err := NewHTTPClient(address)

			if err == nil {
				t.Fatal("invalid proxy accepted")
			}
			if strings.Contains(err.Error(), "secret") {
				t.Fatal("proxy credentials exposed in error")
			}
		})
	}
}

func TestHTTPClientTunnelsHTTPSWithConnect(t *testing.T) {
	clearProxyEnvironment(t)
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "secure response") }))
	defer target.Close()
	connections := make(chan string, 1)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connections <- r.Method + " " + r.Host
		upstream, err := net.DialTimeout("tcp", target.Listener.Addr().String(), time.Second)
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		defer upstream.Close()
		downstream, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		defer downstream.Close()
		_, _ = fmt.Fprint(downstream, "HTTP/1.1 200 Connection Established\r\n\r\n")
		go func() { _, _ = io.Copy(upstream, downstream); upstream.Close() }()
		_, _ = io.Copy(downstream, upstream)
	}))
	defer proxy.Close()
	client, err := NewHTTPClient(proxy.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	trustFixture(t, client, target.Certificate())
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	destination := "secure.example.invalid:443"
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+destination, nil)

	response, requestErr := client.Do(request)
	if requestErr != nil {
		t.Fatal(requestErr)
	}
	body, readErr := io.ReadAll(response.Body)
	response.Body.Close()

	if readErr != nil || string(body) != "secure response" {
		t.Fatalf("HTTPS response=%q error=%v", body, readErr)
	}
	select {
	case connection := <-connections:
		if connection != "CONNECT "+destination {
			t.Fatalf("unexpected tunnel: %s", connection)
		}
	default:
		t.Fatal("CONNECT was not used")
	}
}

func TestHTTPClientDoesNotFollowRedirects(t *testing.T) {
	clearProxyEnvironment(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://example.invalid/", http.StatusFound)
	}))
	defer server.Close()
	client, err := NewHTTPClient("direct")
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()

	response, requestErr := client.Get(server.URL)
	if requestErr != nil {
		t.Fatal(requestErr)
	}
	response.Body.Close()

	if response.StatusCode != http.StatusFound {
		t.Fatal("redirect policy changed")
	}
}

// These fixture-only DNS/CA hooks keep all connections local. Assertions use
// real requests, never Transport.Proxy or the production selection algorithm.
func configureFixtureDestination(t *testing.T, client *http.Client, destination, address string) {
	t.Helper()
	target, _ := url.Parse(destination)
	port := "80"
	if target.Scheme == "https" {
		port = "443"
	}
	destinationAddress := net.JoinHostPort(target.Hostname(), port)
	client.Transport.(*http.Transport).DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if addr == destinationAddress {
			addr = address
		}
		return (&net.Dialer{}).DialContext(ctx, network, addr)
	}
}
func trustFixture(t *testing.T, client *http.Client, certificate *x509.Certificate) {
	t.Helper()
	roots := x509.NewCertPool()
	roots.AddCert(certificate)
	client.Transport.(*http.Transport).TLSClientConfig = &tls.Config{RootCAs: roots, ServerName: "127.0.0.1"}
}
func clearProxyEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range []string{"HTTP_PROXY", "http_proxy", "HTTPS_PROXY", "https_proxy", "NO_PROXY", "no_proxy", "REQUEST_METHOD"} {
		t.Setenv(name, "")
	}
}

type socksRequest struct{ user, password, destination string }

// Minimal local RFC 1929/1928 server: capture routing/authentication, then
// delegate the established connection to the scenario's response stub.
func serveSOCKSFixture(conn net.Conn, captured chan<- socksRequest, serve func(net.Conn)) {
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	read := func(n int) ([]byte, error) { b := make([]byte, n); _, err := io.ReadFull(conn, b); return b, err }
	greeting, err := read(2)
	if err != nil {
		return
	}
	if _, err = read(int(greeting[1])); err != nil {
		return
	}
	if _, err = conn.Write([]byte{5, 2}); err != nil {
		return
	}
	auth, err := read(2)
	if err != nil {
		return
	}
	user, err := read(int(auth[1]))
	if err != nil {
		return
	}
	size, err := read(1)
	if err != nil {
		return
	}
	password, err := read(int(size[0]))
	if err != nil {
		return
	}
	if _, err = conn.Write([]byte{1, 0}); err != nil {
		return
	}
	command, err := read(4)
	if err != nil || command[3] != 3 {
		return
	}
	size, err = read(1)
	if err != nil {
		return
	}
	host, err := read(int(size[0]))
	if err != nil {
		return
	}
	port, err := read(2)
	if err != nil {
		return
	}
	captured <- socksRequest{string(user), string(password), net.JoinHostPort(string(host), fmt.Sprint(int(port[0])*256+int(port[1])))}
	if _, err = conn.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 0}); err != nil {
		return
	}
	serve(conn)
}

func serveHTTPFixtureResponse(conn net.Conn) {
	request, err := http.ReadRequest(bufio.NewReader(conn))
	if err != nil {
		return
	}
	request.Body.Close()
	_, _ = io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Length: 7\r\nConnection: close\r\n\r\nproxied")
}
