// perekat-relay is the server that lets devices reach each other over the
// internet. It is installed on a machine with a public address by perekat
// itself; it can also be run by hand.
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/charg3d/perekat/internal/relay"
)

const usage = `Usage:
  perekat-relay serve [-dir DIR] [-port PORT]   run the server
  perekat-relay info  [-dir DIR]                print what devices need to connect, as JSON
`

// state is what the server keeps between runs.
type state struct {
	Key  string `json:"key"`
	Port int    `json:"port"`
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	fs := flag.NewFlagSet(os.Args[1], flag.ExitOnError)
	dir := fs.String("dir", "/var/lib/perekat-relay", "where the key and certificate are kept")
	port := fs.Int("port", relay.DefaultPort, "TCP port to listen on")
	fs.Parse(os.Args[2:])

	var err error
	switch os.Args[1] {
	case "serve":
		err = serve(*dir, *port)
	case "info":
		err = info(*dir)
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "perekat-relay:", err)
		os.Exit(1)
	}
}

func serve(dir string, port int) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	cert, err := loadOrCreateCert(dir)
	if err != nil {
		return err
	}
	st, err := loadState(dir)
	if errors.Is(err, fs.ErrNotExist) {
		var raw [32]byte
		rand.Read(raw[:])
		st, err = state{Key: hex.EncodeToString(raw[:])}, nil
	}
	if err != nil {
		return err
	}
	st.Port = port
	data, _ := json.Marshal(st)
	if err := os.WriteFile(filepath.Join(dir, "state.json"), data, 0o600); err != nil {
		return err
	}

	ln, err := net.Listen("tcp", ":"+strconv.Itoa(port))
	if err != nil {
		return err
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	log.Info("relay listening", "port", port)
	return (&relay.Server{Key: st.Key, Cert: cert, Log: log}).Serve(ln)
}

func info(dir string) error {
	st, err := loadState(dir)
	if err != nil {
		return err
	}
	cert, err := tls.LoadX509KeyPair(filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem"))
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(relay.Config{
		Port:        st.Port,
		Key:         st.Key,
		Fingerprint: relay.Fingerprint(cert.Certificate[0]),
	})
}

func loadState(dir string) (state, error) {
	var st state
	data, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if err != nil {
		return st, err
	}
	return st, json.Unmarshal(data, &st)
}

func loadOrCreateCert(dir string) (tls.Certificate, error) {
	certPath, keyPath := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err == nil || !errors.Is(err, fs.ErrNotExist) {
		return cert, err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "perekat-relay"},
		NotBefore:    time.Now().AddDate(0, 0, -1),
		NotAfter:     time.Now().AddDate(30, 0, 0),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return tls.Certificate{}, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		return tls.Certificate{}, err
	}
	if err := os.WriteFile(certPath, certPEM, 0o600); err != nil {
		return tls.Certificate{}, err
	}
	return tls.X509KeyPair(certPEM, keyPEM)
}
