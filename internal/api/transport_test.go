package api

import (
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The Chrome hello must complete a real TLS handshake with a stock Go server
// and negotiate HTTP/1.1, the only protocol the transport speaks.
func TestChromeTransportHandshake(t *testing.T) {
	var proto string
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proto = r.Proto
		w.Write([]byte("ok"))
	}))
	srv.EnableHTTP2 = true // offer h2: the client must still pick http/1.1
	srv.StartTLS()
	defer srv.Close()

	roots := x509.NewCertPool()
	roots.AddCert(srv.Certificate())
	c := &http.Client{Transport: ChromeTransport(roots)}
	resp, err := c.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 || proto != "HTTP/1.1" {
		t.Fatalf("status %d proto %q", resp.StatusCode, proto)
	}
}

func TestChromeTransportVerifiesCertificates(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	c := &http.Client{Transport: ChromeTransport(x509.NewCertPool())}
	if _, err := c.Get(srv.URL); err == nil {
		t.Fatal("an untrusted certificate was accepted")
	}
}
