package main

import (
	"crypto/tls"
	"crypto/x509"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	managedTLSRoot    = "/etc/etcd/ssl"
	managedDataRoot   = "/pdata"
	managedBackupRoot = "/data-backup"
)

var managedBackupName = regexp.MustCompile(`^backup_[0-9]+_[0-9]{8}T[0-9]{6}Z$`)

func main() {
	if len(os.Args) < 2 || os.Args[1] == "--help" || os.Args[1] == "-h" {
		usage()
		return
	}

	switch os.Args[1] {
	case "healthcheck-proxy":
		if err := runHealthcheckProxy(os.Args[2:]); err != nil {
			log.Fatal(err)
		}
	case "rolling-backup":
		if err := runRollingBackup(os.Args[2:]); err != nil {
			log.Fatal(err)
		}
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `Etcd Wrapper

Usage:
  etcdwrapper healthcheck-proxy [--port=:2378] [--wait=60s] [--debug=false]
    [--endpoint=https://127.0.0.1:2379/health] [--server-name=etcd.stack]
  etcdwrapper rolling-backup [--period=5m] [--retention=24h] [--index=1]`)
}

func runHealthcheckProxy(args []string) error {
	fs := flag.NewFlagSet("healthcheck-proxy", flag.ExitOnError)
	port := fs.String("port", ":2378", "Port address to serve proxied health checks on")
	wait := fs.Duration("wait", 60*time.Second, "Wait for a period of time before proxying health checks")
	debug := fs.Bool("debug", false, "Verbose logging information for debugging purposes")
	endpoint := fs.String("endpoint", getenv("ETCD_HEALTHCHECK_ENDPOINT", "https://127.0.0.1:2379/health"), "HTTPS etcd health endpoint")
	serverName := fs.String("server-name", getenv("ETCD_HEALTHCHECK_SERVER_NAME", ""), "Expected etcd TLS DNS identity")
	if err := fs.Parse(args); err != nil {
		return err
	}

	healthURL, err := url.Parse(*endpoint)
	if err != nil || healthURL.Scheme != "https" || healthURL.Hostname() == "" || healthURL.User != nil {
		return fmt.Errorf("health endpoint must be an HTTPS URL without user information")
	}

	readyAt := time.Now().Add(*wait)
	client, err := newHealthcheckClient(
		os.Getenv("ETCD_CA_FILE"),
		os.Getenv("ETCD_CERT_FILE"),
		os.Getenv("ETCD_KEY_FILE"),
		*serverName,
	)
	if err != nil {
		return err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if time.Now().Before(readyAt) {
			http.Error(w, "waiting for etcd readiness window", http.StatusServiceUnavailable)
			return
		}
		req, err := http.NewRequest(http.MethodGet, healthURL.String(), nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		resp, err := client.Do(req)
		if err != nil {
			if *debug {
				log.Printf("health proxy failed: %v", err)
			}
			http.Error(w, "HealthCheck failed", http.StatusServiceUnavailable)
			return
		}
		defer resp.Body.Close()
		for k, vals := range resp.Header {
			for _, v := range vals {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/health", http.StatusTemporaryRedirect)
	})

	if *debug {
		log.Printf("starting healthcheck proxy on %s with wait=%s", *port, wait.String())
	}
	return http.ListenAndServe(*port, mux)
}

func newHealthcheckClient(caFile, certFile, keyFile, serverName string) (*http.Client, error) {
	return newHealthcheckClientFromRoot(managedTLSRoot, caFile, certFile, keyFile, serverName)
}

func newHealthcheckClientFromRoot(rootPath, caFile, certFile, keyFile, serverName string) (*http.Client, error) {
	if strings.TrimSpace(caFile) == "" {
		return nil, fmt.Errorf("ETCD_CA_FILE is required")
	}
	if strings.TrimSpace(certFile) == "" || strings.TrimSpace(keyFile) == "" {
		return nil, fmt.Errorf("ETCD_CERT_FILE and ETCD_KEY_FILE are required")
	}
	serverName = strings.TrimSpace(serverName)
	if serverName == "" {
		return nil, fmt.Errorf("ETCD_HEALTHCHECK_SERVER_NAME or --server-name is required")
	}

	root, err := openManagedRoot(rootPath, false, 0)
	if err != nil {
		return nil, fmt.Errorf("open managed etcd TLS root: %w", err)
	}
	defer root.Close()

	caName, _, err := managedPath(rootPath, caFile, false)
	if err != nil {
		return nil, fmt.Errorf("validate ETCD_CA_FILE: %w", err)
	}
	certName, _, err := managedPath(rootPath, certFile, false)
	if err != nil {
		return nil, fmt.Errorf("validate ETCD_CERT_FILE: %w", err)
	}
	keyName, _, err := managedPath(rootPath, keyFile, false)
	if err != nil {
		return nil, fmt.Errorf("validate ETCD_KEY_FILE: %w", err)
	}

	caPEM, err := root.ReadFile(caName)
	if err != nil {
		return nil, fmt.Errorf("read etcd CA: %w", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("ETCD_CA_FILE does not contain a valid certificate")
	}
	certPEM, err := root.ReadFile(certName)
	if err != nil {
		return nil, fmt.Errorf("read etcd client certificate: %w", err)
	}
	keyPEM, err := root.ReadFile(keyName)
	if err != nil {
		return nil, fmt.Errorf("read etcd client key: %w", err)
	}
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("load etcd client certificate: %w", err)
	}

	tlsConfig := &tls.Config{
		MinVersion:   tls.VersionTLS12,
		RootCAs:      roots,
		Certificates: []tls.Certificate{cert},
		ServerName:   serverName,
	}
	return &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			Proxy:           nil,
			TLSClientConfig: tlsConfig,
		},
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return fmt.Errorf("etcd health endpoint redirects are not allowed")
		},
	}, nil
}

func runRollingBackup(args []string) error {
	fs := flag.NewFlagSet("rolling-backup", flag.ExitOnError)
	period := fs.Duration("period", 5*time.Minute, "Backup period")
	retention := fs.Duration("retention", 24*time.Hour, "Backup retention")
	index := fs.Int("index", 1, "Managed service index")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *period <= 0 {
		return fmt.Errorf("period must be positive")
	}

	for {
		if err := createBackup(*index, *retention); err != nil {
			log.Printf("Backup failed: %v", err)
		}
		time.Sleep(*period)
	}
}

func createBackup(index int, retention time.Duration) error {
	return createBackupWithinRoots(
		managedDataRoot,
		getenv("ETCD_DATA_DIR", "/pdata/data.current"),
		managedBackupRoot,
		getenv("ETCD_BACKUP_DIR", "/data-backup"),
		index,
		retention,
		time.Now().UTC(),
		runEtcdBackup,
	)
}

func runEtcdBackup(dataDir, target string) error {
	cmd := exec.Command("etcdctl", "backup", "--data-dir", dataDir, "--backup-dir", target)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func createBackupWithinRoots(dataRoot, dataDir, backupRoot, backupDir string, index int, retention time.Duration, now time.Time, runner func(string, string) error) error {
	if index < 0 {
		return fmt.Errorf("managed service index must not be negative")
	}
	if runner == nil {
		return fmt.Errorf("backup runner is required")
	}

	dataDirName, trustedDataDir, err := managedPath(dataRoot, dataDir, true)
	if err != nil {
		return fmt.Errorf("validate ETCD_DATA_DIR: %w", err)
	}
	dataRootHandle, err := openManagedRoot(dataRoot, false, 0)
	if err != nil {
		return fmt.Errorf("open managed data root: %w", err)
	}
	dataDirectory, err := dataRootHandle.OpenRoot(dataDirName)
	if err != nil {
		dataRootHandle.Close()
		return fmt.Errorf("open managed data directory: %w", err)
	}
	dataDirectory.Close()
	dataRootHandle.Close()
	backupDirName, _, err := managedPath(backupRoot, backupDir, true)
	if err != nil {
		return fmt.Errorf("validate ETCD_BACKUP_DIR: %w", err)
	}

	root, err := openManagedRoot(backupRoot, true, 0o755)
	if err != nil {
		return fmt.Errorf("open managed backup root: %w", err)
	}
	defer root.Close()
	if backupDirName != "." {
		if err := root.MkdirAll(backupDirName, 0o755); err != nil {
			return fmt.Errorf("create managed backup directory: %w", err)
		}
	}

	name := fmt.Sprintf("backup_%d_%s", index, now.UTC().Format("20060102T150405Z"))
	if !managedBackupName.MatchString(name) {
		return fmt.Errorf("generated an invalid managed backup name")
	}
	targetName := filepath.Join(backupDirName, name)
	target := filepath.Join(backupRoot, targetName)
	if err := runner(trustedDataDir, target); err != nil {
		_ = root.RemoveAll(targetName)
		return err
	}
	log.Printf("Created managed backup %s", name)
	return pruneBackups(root, backupDirName, retention, now)
}

func pruneBackups(root *os.Root, directory string, retention time.Duration, now time.Time) error {
	if retention <= 0 {
		return nil
	}
	directoryHandle, err := root.Open(directory)
	if err != nil {
		return err
	}
	defer directoryHandle.Close()
	entries, err := directoryHandle.ReadDir(-1)
	if err != nil {
		return err
	}
	cutoff := now.Add(-retention)
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() || !managedBackupName.MatchString(entry.Name()) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		path := filepath.Join(directory, name)
		if err := root.RemoveAll(path); err != nil {
			return err
		}
	}
	if len(names) > 0 {
		log.Printf("Deleted %d expired managed backups", len(names))
	}
	return nil
}

func managedPath(rootPath, candidate string, allowRoot bool) (string, string, error) {
	rootPath = strings.TrimSpace(rootPath)
	candidate = strings.TrimSpace(candidate)
	if rootPath == "" || candidate == "" || strings.IndexByte(rootPath, 0) >= 0 || strings.IndexByte(candidate, 0) >= 0 {
		return "", "", fmt.Errorf("managed path is empty or contains a NUL byte")
	}
	if strings.ContainsAny(rootPath, "\r\n") || strings.ContainsAny(candidate, "\r\n") || !filepath.IsAbs(rootPath) || !filepath.IsAbs(candidate) {
		return "", "", fmt.Errorf("managed path must be an absolute single-line path")
	}

	rootPath = filepath.Clean(rootPath)
	candidate = filepath.Clean(candidate)
	relative, err := filepath.Rel(rootPath, candidate)
	if err != nil {
		return "", "", err
	}
	if relative == "." {
		if !allowRoot {
			return "", "", fmt.Errorf("managed file path must name a file below its root")
		}
		return relative, rootPath, nil
	}
	if !filepath.IsLocal(relative) || strings.ContainsAny(relative, "\r\n") {
		return "", "", fmt.Errorf("managed path escapes its configured root")
	}
	return relative, filepath.Join(rootPath, relative), nil
}

func openManagedRoot(rootPath string, create bool, mode os.FileMode) (*os.Root, error) {
	rootPath = strings.TrimSpace(rootPath)
	if rootPath == "" || strings.IndexByte(rootPath, 0) >= 0 || strings.ContainsAny(rootPath, "\r\n") || !filepath.IsAbs(rootPath) {
		return nil, fmt.Errorf("managed root must be an absolute single-line path")
	}
	rootPath = filepath.Clean(rootPath)
	if create {
		if err := os.MkdirAll(rootPath, mode); err != nil {
			return nil, err
		}
	}
	info, err := os.Lstat(rootPath)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, fmt.Errorf("managed root must be a real directory")
	}
	return os.OpenRoot(rootPath)
}

func getenv(name, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		return v
	}
	return fallback
}
