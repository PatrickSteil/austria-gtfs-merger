package dbptls

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type testCert struct {
	cert *x509.Certificate
	der  []byte
	key  *ecdsa.PrivateKey
}

func newCert(t *testing.T, tmpl, parent *x509.Certificate, parentKey *ecdsa.PrivateKey) testCert {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if parent == nil {
		parent, parentKey = tmpl, key
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, &key.PublicKey, parentKey)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return testCert{cert: cert, der: der, key: key}
}

func caTemplate(serial int64, cn string) *x509.Certificate {
	return &x509.Certificate{
		SerialNumber:          big.NewInt(serial),
		Subject:               pkix.Name{CommonName: cn},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
}

// startServerWithMissingIntermediate serves a leaf signed by an intermediate
// but, like the DBP servers, sends only the leaf. It returns the server and
// the root and intermediate certificates.
func startServerWithMissingIntermediate(t *testing.T) (*httptest.Server, *x509.Certificate, *x509.Certificate) {
	t.Helper()
	root := newCert(t, caTemplate(1, "test root"), nil, nil)
	inter := newCert(t, caTemplate(2, "test intermediate"), root.cert, root.key)
	leaf := newCert(t, &x509.Certificate{
		SerialNumber: big.NewInt(3),
		Subject:      pkix.Name{CommonName: "leaf"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}, inter.cert, inter.key)

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{{
		Certificate: [][]byte{leaf.der}, // no intermediate
		PrivateKey:  leaf.key,
	}}}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv, root.cert, inter.cert
}

func TestServerWithoutIntermediateFailsWithoutExtraCert(t *testing.T) {
	srv, root, _ := startServerWithMissingIntermediate(t)
	pool := x509.NewCertPool()
	pool.AddCert(root)

	client := &http.Client{Transport: transportWith(pool, nil)}
	if _, err := client.Get(srv.URL); err == nil {
		t.Fatal("expected certificate verification error, got nil")
	}
}

func TestExtraIntermediateFixesVerification(t *testing.T) {
	srv, root, inter := startServerWithMissingIntermediate(t)
	pool := x509.NewCertPool()
	pool.AddCert(root)

	client := &http.Client{Transport: transportWith(pool, []*x509.Certificate{inter})}
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	resp.Body.Close()
}

func TestEmbeddedIntermediateParses(t *testing.T) {
	certs, err := embeddedIntermediates()
	if err != nil {
		t.Fatal(err)
	}
	if len(certs) == 0 {
		t.Fatal("no embedded certificates")
	}
	for _, c := range certs {
		if !c.IsCA {
			t.Errorf("%s is not a CA certificate", c.Subject)
		}
		if time.Now().After(c.NotAfter) {
			t.Errorf("%s expired on %s", c.Subject, c.NotAfter)
		}
	}
}
