package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"log"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewHealthcheckClientVerifiesManagedStackIdentity(t *testing.T) {
	caFile, certFile, keyFile, serverCertificate := writeTLSFixture(t, "etcd.test-stack")
	client, err := newHealthcheckClient(caFile, certFile, keyFile, "etcd.test-stack")
	if err != nil {
		t.Fatalf("newHealthcheckClient returned an error: %v", err)
	}

	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("unexpected transport type %T", client.Transport)
	}
	if transport.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("TLS verification must never be disabled")
	}
	if transport.TLSClientConfig.MinVersion != tls.VersionTLS12 {
		t.Fatalf("minimum TLS version = %d, want TLS 1.2", transport.TLSClientConfig.MinVersion)
	}
	if transport.TLSClientConfig.ServerName != "etcd.test-stack" {
		t.Fatalf("server name = %q", transport.TLSClientConfig.ServerName)
	}

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"health": "true"}`))
	}))
	caPEM, err := os.ReadFile(caFile)
	if err != nil {
		t.Fatal(err)
	}
	clientRoots := x509.NewCertPool()
	if !clientRoots.AppendCertsFromPEM(caPEM) {
		t.Fatal("failed to load test client CA")
	}
	server.TLS = &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{serverCertificate},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    clientRoots,
	}
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.StartTLS()
	t.Cleanup(server.Close)

	response, err := client.Get(server.URL)
	if err != nil {
		t.Fatalf("verified TLS health request failed: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("health status = %d", response.StatusCode)
	}

	wrongNameClient, err := newHealthcheckClient(caFile, certFile, keyFile, "wrong.test-stack")
	if err != nil {
		t.Fatalf("wrong-name client setup failed unexpectedly: %v", err)
	}
	if _, err := wrongNameClient.Get(server.URL); err == nil {
		t.Fatal("TLS request with the wrong managed-stack identity unexpectedly succeeded")
	}
}

func TestNewHealthcheckClientRejectsIncompleteTrustMaterial(t *testing.T) {
	caFile, certFile, keyFile, _ := writeTLSFixture(t, "etcd.test-stack")
	invalidCA := filepath.Join(t.TempDir(), "invalid-ca.pem")
	if err := os.WriteFile(invalidCA, []byte("not a certificate"), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name       string
		caFile     string
		certFile   string
		keyFile    string
		serverName string
		want       string
	}{
		{name: "missing CA", certFile: certFile, keyFile: keyFile, serverName: "etcd.test-stack", want: "ETCD_CA_FILE is required"},
		{name: "missing certificate", caFile: caFile, keyFile: keyFile, serverName: "etcd.test-stack", want: "ETCD_CERT_FILE and ETCD_KEY_FILE are required"},
		{name: "missing key", caFile: caFile, certFile: certFile, serverName: "etcd.test-stack", want: "ETCD_CERT_FILE and ETCD_KEY_FILE are required"},
		{name: "missing server name", caFile: caFile, certFile: certFile, keyFile: keyFile, want: "ETCD_HEALTHCHECK_SERVER_NAME"},
		{name: "invalid CA", caFile: invalidCA, certFile: certFile, keyFile: keyFile, serverName: "etcd.test-stack", want: "does not contain a valid certificate"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := newHealthcheckClient(test.caFile, test.certFile, test.keyFile, test.serverName)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want text %q", err, test.want)
			}
		})
	}
}

func writeTLSFixture(t *testing.T, serverName string) (string, string, string, tls.Certificate) {
	t.Helper()
	now := time.Now()
	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "PastureStack test CA"},
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              now.Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}

	leafKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	leafTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: serverName},
		DNSNames:     []string{serverName},
		NotBefore:    now.Add(-time.Minute),
		NotAfter:     now.Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTemplate, caTemplate, &leafKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}

	directory := t.TempDir()
	caFile := filepath.Join(directory, "ca.pem")
	certFile := filepath.Join(directory, "cert.pem")
	keyFile := filepath.Join(directory, "key.pem")
	writePEMFile(t, caFile, "CERTIFICATE", caDER)
	writePEMFile(t, certFile, "CERTIFICATE", leafDER)
	writePEMFile(t, keyFile, "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(leafKey))

	serverCertificate, err := tls.X509KeyPair(
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER}),
		pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(leafKey)}),
	)
	if err != nil {
		t.Fatal(err)
	}
	return caFile, certFile, keyFile, serverCertificate
}

func writePEMFile(t *testing.T, path, blockType string, data []byte) {
	t.Helper()
	encoded := pem.EncodeToMemory(&pem.Block{Type: blockType, Bytes: data})
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
}
