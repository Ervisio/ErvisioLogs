package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// tlsCert writes a self-signed certificate and key for the bundled PostgreSQL of the Compose stack, so the app can
// connect with sslmode=verify-full (the certificate is its own root). It keeps existing files: the certificate of a
// running stack does not change on every start.
func tlsCert(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("tls-cert", flag.ContinueOnError)
	dir := fs.String("dir", "", "directory for server.crt and server.key")
	hosts := fs.String("host", "postgres", "comma-separated DNS names for the certificate")
	days := fs.Int("days", 3650, "validity in days")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dir == "" {
		return errors.New("tls-cert: --dir is required")
	}
	var names []string
	for _, n := range strings.Split(*hosts, ",") {
		if n = strings.TrimSpace(n); n != "" {
			names = append(names, n)
		}
	}
	if len(names) == 0 || *days < 1 {
		return errors.New("tls-cert: --host needs at least one name and --days must be positive")
	}
	crtPath, keyPath := filepath.Join(*dir, "server.crt"), filepath.Join(*dir, "server.key")
	_, crtErr := os.Stat(crtPath)
	_, keyErr := os.Stat(keyPath)
	if crtErr == nil && keyErr == nil {
		fmt.Fprintln(stdout, "tls-cert: keeping the existing certificate in", *dir)
		return nil
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return err
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: names[0], Organization: []string{"Ervisio Logs"}},
		DNSNames:              names,
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.AddDate(0, 0, *days),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true, // self-signed: the client uses this certificate as its root
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(*dir, 0o755); err != nil {
		return err
	}
	// Key first: a certificate without its key would be kept by the next run.
	if err := writeFileAtomic(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		return err
	}
	if err := writeFileAtomic(crtPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "tls-cert: wrote %s and %s for %s\n", crtPath, keyPath, strings.Join(names, ", "))
	return nil
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	if err := os.Chmod(tmp, mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
