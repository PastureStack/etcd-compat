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
	"sort"
	"strings"
	"time"
)

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

	caPEM, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("read etcd CA: %w", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("ETCD_CA_FILE does not contain a valid certificate")
	}
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
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
	dataDir := getenv("ETCD_DATA_DIR", "/pdata/data.current")
	backupRoot := getenv("ETCD_BACKUP_DIR", "/data-backup")
	if err := os.MkdirAll(backupRoot, 0o755); err != nil {
		return err
	}

	name := fmt.Sprintf("backup_%d_%s", index, time.Now().UTC().Format("20060102T150405Z"))
	target := filepath.Join(backupRoot, name)
	cmd := exec.Command("etcdctl", "backup", "--data-dir", dataDir, "--backup-dir", target)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		_ = os.RemoveAll(target)
		return err
	}
	log.Printf("Created backup %s", target)
	return pruneBackups(backupRoot, retention)
}

func pruneBackups(root string, retention time.Duration) error {
	if retention <= 0 {
		return nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	cutoff := time.Now().Add(-retention)
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "backup_") {
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
		path := filepath.Join(root, name)
		if err := os.RemoveAll(path); err != nil {
			return err
		}
		log.Printf("Deleted backup %s", path)
	}
	return nil
}

func getenv(name, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		return v
	}
	return fallback
}
