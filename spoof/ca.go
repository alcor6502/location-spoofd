package spoof

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"sync"
	"time"
)

// CAName is the CommonName shown in iOS Settings > General > About > Certificate Trust Settings.
const CAName = "Location Spoofer CA"

// CAValidity is 10 years: Apple's 825/398-day limits apply to leaf TLS certificates,
// not to a root the user installs and trusts manually. Leaves are minted for 30 days.
const CAValidity = 10 * 365 * 24 * time.Hour

// GenerateCA creates a fresh self-signed root as PEM cert + PKCS#8 key.
func GenerateCA() (certPEM, keyPEM []byte, err error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, err
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Location Spoofer"},
			Country:      []string{"US"},
			CommonName:   CAName,
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(CAValidity),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return nil, nil, err
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	privateKeyDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return nil, nil, err
	}
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateKeyDER})
	return certPEM, keyPEM, nil
}

// ParseCA loads a PEM cert/key pair and fills Leaf so the CA can sign host certificates.
func ParseCA(certPEM, keyPEM []byte) (*tls.Certificate, error) {
	parsed, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, err
	}
	if parsed.Leaf, err = x509.ParseCertificate(parsed.Certificate[0]); err != nil {
		return nil, err
	}
	return &parsed, nil
}

// CertPEM returns the CA certificate alone, the file the phone has to install.
func CertPEM(ca *tls.Certificate) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ca.Certificate[0]})
}

// HostCerts mints and caches leaf certificates signed by the CA, one per server name.
type HostCerts struct {
	ca    *tls.Certificate
	mu    sync.Mutex
	cache map[string]*tls.Certificate
}

func NewHostCerts(ca *tls.Certificate) *HostCerts {
	return &HostCerts{ca: ca, cache: make(map[string]*tls.Certificate)}
}

// Get returns a certificate for host, reusing a cached one when available.
func (h *HostCerts) Get(host string) (*tls.Certificate, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if c, ok := h.cache[host]; ok && time.Now().Before(c.Leaf.NotAfter.Add(-24*time.Hour)) {
		return c, nil
	}

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil {
		return nil, err
	}
	template := x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: host},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(30 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	if ip := net.ParseIP(host); ip != nil {
		template.IPAddresses = []net.IP{ip}
	} else {
		template.DNSNames = []string{host}
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, h.ca.Leaf, &key.PublicKey, h.ca.PrivateKey)
	if err != nil {
		return nil, err
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	c := &tls.Certificate{
		Certificate: [][]byte{der, h.ca.Certificate[0]},
		PrivateKey:  key,
		Leaf:        leaf,
	}
	h.cache[host] = c
	return c, nil
}

// TLSConfig returns a server config that presents a CA-signed certificate for whatever SNI the client sends.
func (h *HostCerts) TLSConfig() *tls.Config {
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		NextProtos: []string{"http/1.1"},
		GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
			name := hello.ServerName
			if name == "" {
				name = hello.Conn.LocalAddr().(*net.TCPAddr).IP.String()
			}
			return h.Get(name)
		},
	}
}
