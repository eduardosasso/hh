// Package trust checks and installs the internal root certificate.
package trust

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"

	"github.com/smallstep/truststore"

	"github.com/eduardosasso/hh/internal/home"
)

// Root reads the internal CA root certificate.
func Root() (*x509.Certificate, error) {
	path, err := home.RootCert()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read root certificate: %w", err)
	}
	block, _ := pem.Decode(b)
	if block == nil {
		return nil, fmt.Errorf("%s: no PEM block", path)
	}
	c, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse root certificate: %w", err)
	}

	return c, nil
}

// Trusted reports whether the OS trust store accepts the root.
func Trusted() (bool, error) {
	c, err := Root()
	if err != nil {
		return false, err
	}
	// Any verify error means the OS does not anchor the root; the platform
	// verifiers report that with different error types.
	_, err = c.Verify(x509.VerifyOptions{})

	return err == nil, nil
}

// Install adds the root to the OS trust store. It runs sudo.
func Install() error {
	path, err := home.RootCert()
	if err != nil {
		return err
	}
	if err := truststore.InstallFile(path); err != nil {
		return fmt.Errorf("trust %s: %w", path, err)
	}

	return nil
}
