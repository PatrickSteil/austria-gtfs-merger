// Package dbptls provides an HTTP transport for the Mobilitätsverbünde (DBP)
// servers. They send only their leaf certificate and omit the GoDaddy
// intermediate. Browsers fetch it via AIA, but Go does not, so verification
// fails with "certificate signed by unknown authority" on runners that lack
// the intermediate (e.g. GitHub Actions).
package dbptls

import (
	"crypto/tls"
	"crypto/x509"
	_ "embed"
	"encoding/pem"
	"fmt"
	"net/http"
)

// godaddy-g2-intermediate.pem is "Go Daddy Secure Certificate Authority - G2"
// (valid until 2031-05-03), from http://certificates.godaddy.com/repository/gdig2.crt.
// It is signed by the Go Daddy Root CA - G2, which is in the system roots.
//
//go:embed godaddy-g2-intermediate.pem
var intermediatePEM []byte

// Transport returns a clone of http.DefaultTransport that verifies against the
// system roots plus the embedded intermediate.
func Transport() http.RoundTripper {
	pool, err := x509.SystemCertPool()
	if err != nil {
		pool = x509.NewCertPool()
	}
	extra, err := embeddedIntermediates()
	if err != nil {
		panic(err) // the embedded file is a build-time constant
	}
	return transportWith(pool, extra)
}

// transportWith trusts the extra certificates in addition to the given pool.
// Verification is unchanged otherwise: the leaf must still chain to a root.
func transportWith(pool *x509.CertPool, extra []*x509.Certificate) *http.Transport {
	roots := pool.Clone()
	for _, c := range extra {
		roots.AddCert(c)
	}
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	return t
}

func embeddedIntermediates() ([]*x509.Certificate, error) {
	var certs []*x509.Certificate
	rest := intermediatePEM
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		c, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parse embedded certificate: %w", err)
		}
		certs = append(certs, c)
	}
	return certs, nil
}
