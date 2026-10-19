package outbound

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/proxy"
)

func proxyDialer(endpoint *url.URL, username, password string) (DialFunc, error) {
	port := endpoint.Port()
	if port == "" {
		port = map[string]string{"http": "80", "https": "443", "socks5": "1080", "socks5h": "1080"}[endpoint.Scheme]
	}
	address := net.JoinHostPort(endpoint.Hostname(), port)
	direct := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	var connect DialFunc
	if endpoint.Scheme == "socks5" || endpoint.Scheme == "socks5h" {
		var auth *proxy.Auth
		if username != "" {
			if len(username) > 255 || len(password) > 255 {
				return nil, ErrUnavailable
			}
			auth = &proxy.Auth{User: username, Password: password}
		}
		dialer, err := proxy.SOCKS5("tcp", address, auth, direct)
		if err != nil {
			return nil, ErrUnavailable
		}
		contextDialer, ok := dialer.(proxy.ContextDialer)
		if !ok {
			return nil, ErrUnavailable
		}
		connect = contextDialer.DialContext
	} else {
		if strings.Contains(username, ":") {
			return nil, ErrUnavailable
		}
		connect = func(ctx context.Context, network, target string) (net.Conn, error) {
			conn, err := direct.DialContext(ctx, network, address)
			if err != nil {
				return nil, err
			}
			complete := false
			defer func() {
				if !complete {
					conn.Close()
				}
			}()
			raw := conn
			stop := context.AfterFunc(ctx, func() { raw.Close() })
			defer stop()
			deadline := time.Now().Add(10 * time.Second)
			if limit, ok := ctx.Deadline(); ok && limit.Before(deadline) {
				deadline = limit
			}
			if err := conn.SetDeadline(deadline); err != nil {
				return nil, err
			}
			if endpoint.Scheme == "https" {
				secure := tls.Client(conn, &tls.Config{ServerName: endpoint.Hostname(), MinVersion: tls.VersionTLS12})
				if err := secure.HandshakeContext(ctx); err != nil {
					return nil, err
				}
				conn = secure
			}
			req := &http.Request{Method: http.MethodConnect, URL: &url.URL{Opaque: target}, Host: target, Header: make(http.Header)}
			if username != "" {
				req.Header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(username+":"+password)))
			}
			if err := req.Write(conn); err != nil {
				return nil, err
			}
			limited := &io.LimitedReader{R: conn, N: 64 << 10}
			reader := bufio.NewReaderSize(limited, 16<<10)
			response, err := http.ReadResponse(reader, req)
			if err != nil || response.StatusCode != http.StatusOK {
				return nil, ErrUnavailable
			}
			if err := conn.SetDeadline(time.Time{}); err != nil {
				return nil, err
			}
			limited.N = 1<<63 - 1
			complete = true
			return &bufferedConn{Conn: conn, reader: reader}, nil
		}
	}
	return func(ctx context.Context, network, target string) (net.Conn, error) {
		conn, err := connect(ctx, network, target)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, ErrUnavailable
		}
		return conn, nil
	}, nil
}

type bufferedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *bufferedConn) Read(buffer []byte) (int, error) { return c.reader.Read(buffer) }
