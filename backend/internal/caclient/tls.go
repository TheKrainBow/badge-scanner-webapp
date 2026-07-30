package caclient

import (
	"crypto/tls"
	"crypto/x509"
	"embed"
	"encoding/pem"
	"fmt"
)

//go:embed ca_cert.pem
var embeddedFS embed.FS

// pinnedTLSConfig ports mobile/app/.../api/CaTrust.kt: the CA (Ixoff ibox4)
// serves a self-signed certificate whose CN doesn't match the endpoint
// hostname, so default verification rejects the handshake. This trusts that
// exact pinned certificate as a fallback; any other server still goes
// through normal system trust.
func pinnedTLSConfig() (*tls.Config, error) {
	pemBytes, err := embeddedFS.ReadFile("ca_cert.pem")
	if err != nil {
		return nil, fmt.Errorf("read embedded ca_cert.pem: %w", err)
	}
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("ca_cert.pem: no PEM block found")
	}
	pinned, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse pinned cert: %w", err)
	}

	return &tls.Config{
		// Skip Go's built-in verification so we can run it ourselves below
		// and fall back to the pinned cert — mirrors the Kotlin combined
		// X509TrustManager (system check, then pinned-cert check).
		InsecureSkipVerify: true,
		VerifyConnection: func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 {
				return fmt.Errorf("no peer certificates presented")
			}
			leaf := cs.PeerCertificates[0]

			// 1) Normal system trust + hostname check.
			intermediates := x509.NewCertPool()
			for _, c := range cs.PeerCertificates[1:] {
				intermediates.AddCert(c)
			}
			if _, err := leaf.Verify(x509.VerifyOptions{
				DNSName:       cs.ServerName,
				Intermediates: intermediates,
			}); err == nil {
				return nil
			}

			// 2) Fall back to an exact match against the pinned cert,
			// regardless of hostname (same as the Kotlin hostname
			// verifier's fallback).
			if leaf.Equal(pinned) {
				return nil
			}
			return fmt.Errorf("certificate does not match the pinned CA certificate")
		},
	}, nil
}
