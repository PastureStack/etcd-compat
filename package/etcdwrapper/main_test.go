package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
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
	root := filepath.Dir(caFile)
	client, err := newHealthcheckClientFromRoot(root, caFile, certFile, keyFile, "etcd.test-stack")
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

	wrongNameClient, err := newHealthcheckClientFromRoot(root, caFile, certFile, keyFile, "wrong.test-stack")
	if err != nil {
		t.Fatalf("wrong-name client setup failed unexpectedly: %v", err)
	}
	if _, err := wrongNameClient.Get(server.URL); err == nil {
		t.Fatal("TLS request with the wrong managed-stack identity unexpectedly succeeded")
	}
}

func TestNewHealthcheckClientRejectsIncompleteTrustMaterial(t *testing.T) {
	caFile, certFile, keyFile, _ := writeTLSFixture(t, "etcd.test-stack")
	invalidCA := filepath.Join(filepath.Dir(caFile), "invalid-ca.pem")
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
			_, err := newHealthcheckClientFromRoot(filepath.Dir(caFile), test.caFile, test.certFile, test.keyFile, test.serverName)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want text %q", err, test.want)
			}
		})
	}
}

func TestNewHealthcheckClientRejectsPathsOutsideManagedRoot(t *testing.T) {
	caFile, certFile, keyFile, _ := writeTLSFixture(t, "etcd.test-stack")
	managedRoot := filepath.Dir(caFile)
	outsideRoot := t.TempDir()
	outsideCA := filepath.Join(outsideRoot, "outside-ca.pem")
	caPEM, err := os.ReadFile(caFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outsideCA, caPEM, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := newHealthcheckClientFromRoot(managedRoot, outsideCA, certFile, keyFile, "etcd.test-stack"); err == nil || !strings.Contains(err.Error(), "escapes") {
		t.Fatalf("outside CA error = %v, want managed-root rejection", err)
	}
	if _, err := newHealthcheckClientFromRoot(managedRoot, caFile+"\nignored", certFile, keyFile, "etcd.test-stack"); err == nil || !strings.Contains(err.Error(), "single-line") {
		t.Fatalf("newline CA error = %v, want single-line rejection", err)
	}

	symlinkCA := filepath.Join(managedRoot, "symlink-ca.pem")
	if err := os.Symlink(outsideCA, symlinkCA); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}
	if _, err := newHealthcheckClientFromRoot(managedRoot, symlinkCA, certFile, keyFile, "etcd.test-stack"); err == nil {
		t.Fatal("managed-root symlink escape unexpectedly succeeded")
	}
	symlinkRoot := filepath.Join(t.TempDir(), "tls-root")
	if err := os.Symlink(managedRoot, symlinkRoot); err != nil {
		t.Skipf("root symlinks are unavailable: %v", err)
	}
	if _, err := newHealthcheckClientFromRoot(symlinkRoot, filepath.Join(symlinkRoot, filepath.Base(caFile)), filepath.Join(symlinkRoot, filepath.Base(certFile)), filepath.Join(symlinkRoot, filepath.Base(keyFile)), "etcd.test-stack"); err == nil {
		t.Fatal("symlinked TLS root unexpectedly succeeded")
	}
}

func TestCreateBackupWithinRootsAndPruneExpiredManagedBackups(t *testing.T) {
	now := time.Date(2026, time.August, 14, 3, 4, 5, 0, time.UTC)
	dataRoot := t.TempDir()
	dataDir := filepath.Join(dataRoot, "data.current")
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	backupRoot := t.TempDir()
	expired := filepath.Join(backupRoot, "backup_1_20260812T030405Z")
	unmanaged := filepath.Join(backupRoot, "backup_escape")
	for _, directory := range []string{expired, unmanaged} {
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	old := now.Add(-48 * time.Hour)
	if err := os.Chtimes(expired, old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(unmanaged, old, old); err != nil {
		t.Fatal(err)
	}

	called := false
	runner := func(gotDataDir, target string) error {
		called = true
		if gotDataDir != dataDir {
			t.Fatalf("data directory = %q, want %q", gotDataDir, dataDir)
		}
		wantTarget := filepath.Join(backupRoot, "backup_7_20260814T030405Z")
		if target != wantTarget {
			t.Fatalf("target = %q, want %q", target, wantTarget)
		}
		return os.MkdirAll(target, 0o700)
	}
	if err := createBackupWithinRoots(dataRoot, dataDir, backupRoot, backupRoot, 7, 24*time.Hour, now, runner); err != nil {
		t.Fatalf("createBackupWithinRoots returned an error: %v", err)
	}
	if !called {
		t.Fatal("backup runner was not called")
	}
	if _, err := os.Stat(expired); !os.IsNotExist(err) {
		t.Fatalf("expired managed backup still exists: %v", err)
	}
	if _, err := os.Stat(unmanaged); err != nil {
		t.Fatalf("unmanaged directory was removed: %v", err)
	}
}

func TestCreateBackupWithinRootsRejectsEscapes(t *testing.T) {
	dataRoot := t.TempDir()
	dataDir := filepath.Join(dataRoot, "data.current")
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	backupRoot := t.TempDir()
	outside := t.TempDir()
	runnerCalled := false
	runner := func(_, _ string) error {
		runnerCalled = true
		return nil
	}

	tests := []struct {
		name      string
		dataDir   string
		backupDir string
	}{
		{name: "data directory outside root", dataDir: outside, backupDir: backupRoot},
		{name: "backup directory outside root", dataDir: dataDir, backupDir: outside},
		{name: "newline in backup directory", dataDir: dataDir, backupDir: backupRoot + "\nignored"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := createBackupWithinRoots(dataRoot, test.dataDir, backupRoot, test.backupDir, 1, time.Hour, time.Now(), runner); err == nil {
				t.Fatal("unsafe managed path unexpectedly succeeded")
			}
		})
	}
	if runnerCalled {
		t.Fatal("backup runner was called for an unsafe path")
	}

	link := filepath.Join(backupRoot, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}
	if err := createBackupWithinRoots(dataRoot, dataDir, backupRoot, link, 1, time.Hour, time.Now(), runner); err == nil {
		t.Fatal("backup symlink escape unexpectedly succeeded")
	}
	if runnerCalled {
		t.Fatal("backup runner was called through a symlink escape")
	}
	symlinkRoot := filepath.Join(t.TempDir(), "backup-root")
	if err := os.Symlink(backupRoot, symlinkRoot); err != nil {
		t.Skipf("root symlinks are unavailable: %v", err)
	}
	if err := createBackupWithinRoots(dataRoot, dataDir, symlinkRoot, symlinkRoot, 1, time.Hour, time.Now(), runner); err == nil {
		t.Fatal("symlinked backup root unexpectedly succeeded")
	}
	if runnerCalled {
		t.Fatal("backup runner was called through a symlinked root")
	}
}

func TestCreateBackupWithinRootsCleansOnlyFailedTarget(t *testing.T) {
	now := time.Date(2026, time.August, 14, 4, 5, 6, 0, time.UTC)
	dataRoot := t.TempDir()
	dataDir := filepath.Join(dataRoot, "data.current")
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	backupRoot := t.TempDir()
	preserved := filepath.Join(backupRoot, "backup_4_20260813T040506Z")
	if err := os.Mkdir(preserved, 0o700); err != nil {
		t.Fatal(err)
	}
	wantErr := errors.New("simulated backup failure")
	runner := func(_, target string) error {
		if err := os.MkdirAll(target, 0o700); err != nil {
			t.Fatal(err)
		}
		return wantErr
	}
	err := createBackupWithinRoots(dataRoot, dataDir, backupRoot, backupRoot, 5, time.Hour, now, runner)
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want %v", err, wantErr)
	}
	failedTarget := filepath.Join(backupRoot, "backup_5_20260814T040506Z")
	if _, err := os.Stat(failedTarget); !os.IsNotExist(err) {
		t.Fatalf("failed target still exists: %v", err)
	}
	if _, err := os.Stat(preserved); err != nil {
		t.Fatalf("existing backup was removed: %v", err)
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
