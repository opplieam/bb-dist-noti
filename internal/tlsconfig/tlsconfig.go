package tlsconfig

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"path/filepath"

	"github.com/opplieam/bb-dist-noti/pkg/projectpath"
)

var (
	CAFile         = getPath("ca.pem")
	ServerCertFile = getPath("server.pem")
	ServerKeyFile  = getPath("server-key.pem")

	ClientCertFile = getPath("client.pem")
	ClientKeyFile  = getPath("client-key.pem")
)

// getPath returns the full path to a file within the project's tls directory
func getPath(filename string) string {
	projectRoot, err := projectpath.GetProjectRoot()
	if err != nil {
		panic(err)
	}
	return filepath.Join(projectRoot, "tls", filename)
}

// -------------------------------------------------------

type TLSConfig struct {
	CertFile      string
	KeyFile       string
	CAFile        string
	ServerAddress string
	Server        bool
}

// SetupTLSConfig configures a tls.Config based on the provided TLSConfig struct.
// This function supports mutual TLS, where both client and server authenticate each other using certificates.
func SetupTLSConfig(cfg TLSConfig) (*tls.Config, error) {
	var err error
	tlsConfig := &tls.Config{}

	// Load certificate and private key if both paths are provided
	if cfg.CertFile != "" && cfg.KeyFile != "" {
		tlsConfig.Certificates = make([]tls.Certificate, 1)
		tlsConfig.Certificates[0], err = tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
		if err != nil {
			return nil, err
		}
	}
	// Load CA certificate and configure TLS settings based on whether it's a server or client
	if cfg.CAFile != "" {
		b, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, err
		}
		ca := x509.NewCertPool()
		ok := ca.AppendCertsFromPEM(b)
		if !ok {
			return nil, fmt.Errorf("failed to parse root certificate: %q", cfg.CAFile)
		}
		// If it's a server configuration, set up client authentication
		if cfg.Server {
			tlsConfig.ClientCAs = ca
			tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert
		} else { // If it's a client configuration, set up root CAs for verification
			tlsConfig.RootCAs = ca
		}
		tlsConfig.ServerName = cfg.ServerAddress
	}
	return tlsConfig, nil
}
