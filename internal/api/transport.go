package api

import (
	"context"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"time"

	utls "github.com/refraction-networking/utls"
)

// chromeHello is the ClientHello the transport presents. obramat.es is behind
// DataDome, which scores the TLS fingerprint: Go's own handshake beside a real
// browser cookie and user agent is the inconsistent pair replaying a session
// exists to avoid. See docs/adr/0004-chrome-tls-fingerprint.md.
var chromeHello = utls.HelloChrome_Auto

// ChromeTransport returns an http.Transport whose TLS handshake is Chrome's.
// roots is nil in production (system roots) and a test CA in tests.
//
// ALPN is pinned to http/1.1 so a plain http.Transport can carry the
// connection. JA3 hashes extension types, not ALPN values, so the JA3 stays
// Chrome's; JA4 does encode the first ALPN value ("h1" instead of Chrome's
// "h2"). If that ever matters, switch here to an HTTP/2 transport; nothing
// else moves.
func ChromeTransport(roots *x509.CertPool) *http.Transport {
	dialer := &net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}
	return &http.Transport{
		// No proxy: net/http only runs DialTLSContext for direct HTTPS, so a
		// proxy from HTTPS_PROXY would silently swap in Go's own hello.
		Proxy: nil,
		DialTLSContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			raw, err := dialer.DialContext(ctx, network, addr)
			if err != nil {
				return nil, err
			}
			host, _, err := net.SplitHostPort(addr)
			if err != nil {
				raw.Close()
				return nil, err
			}
			conn, err := chromeHandshake(ctx, raw, host, roots)
			if err != nil {
				raw.Close()
				return nil, err
			}
			return conn, nil
		},
		ForceAttemptHTTP2:     false,
		MaxIdleConns:          4,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
	}
}

func chromeHandshake(ctx context.Context, raw net.Conn, host string, roots *x509.CertPool) (net.Conn, error) {
	spec, err := utls.UTLSIdToSpec(chromeHello)
	if err != nil {
		return nil, fmt.Errorf("chrome hello spec: %w", err)
	}
	for _, ext := range spec.Extensions {
		if alpn, ok := ext.(*utls.ALPNExtension); ok {
			alpn.AlpnProtocols = []string{"http/1.1"}
		}
	}
	conn := utls.UClient(raw, &utls.Config{ServerName: host, RootCAs: roots}, utls.HelloCustom)
	if err := conn.ApplyPreset(&spec); err != nil {
		return nil, fmt.Errorf("chrome hello preset: %w", err)
	}
	if err := conn.HandshakeContext(ctx); err != nil {
		return nil, err
	}
	return conn, nil
}
