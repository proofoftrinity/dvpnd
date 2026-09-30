// SPDX-License-Identifier: Apache-2.0

package common

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSelfSignedCertificateAndPin(t *testing.T) {
	certPEM, keyPEM, err := SelfSignedCertificate("dvpnd", time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	block, _ := pem.Decode(certPEM)
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if cert.Subject.CommonName != "dvpnd" {
		t.Fatalf("CN = %q", cert.Subject.CommonName)
	}
	if kb, _ := pem.Decode(keyPEM); kb == nil || kb.Type != "PRIVATE KEY" {
		t.Fatal("key is not PKCS#8 PEM")
	}

	path := filepath.Join(t.TempDir(), "tls.crt")
	if err := os.WriteFile(path, certPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	pin, err := CertificatePin(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(cert.Raw)
	if pin != hex.EncodeToString(sum[:]) || len(pin) != 64 {
		t.Fatalf("pin = %s", pin)
	}

	if _, err := CertificatePin(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("missing file accepted")
	}

	colon := ColonPin(pin)
	if len(colon) != 95 || strings.ReplaceAll(colon, ":", "") != pin || colon[2] != ':' {
		t.Fatalf("colon pin = %s", colon)
	}
}
