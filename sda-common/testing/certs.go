package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"flag"
	"math/big"
	"net"
	"os"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
)

func main() {
	var outDir string
	host := flag.String("host", "", "Comma-separated hostnames to generate a certificate for")
	path := flag.String("path", "", "Path where to write the certificates")

	flag.Parse()
	if *path == "" {
		outDir, _ = os.MkdirTemp("", "gocerts")
	} else {
		err := os.MkdirAll(*path, 0777)
		if err != nil {
			log.Fatalln(err)
		}
		outDir = *path
	}

	// set up our CA certificate
	caTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2000),
		Subject: pkix.Name{
			Organization: []string{"NEIC"},
			CommonName:   "Root CA",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().AddDate(0, 0, 1),
		KeyUsage:              x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	// create our private and public key
	caPrivKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		log.Fatalln(err)
	}

	// create the CA certificate
	caBytes, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caPrivKey.PublicKey, caPrivKey)
	if err != nil {
		log.Fatalln(err)
	}

	err = TLScertToFile(outDir+"/ca.crt", caBytes)
	if err != nil {
		log.Fatalln(err)
	}

	tlsKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		log.Fatalln(err)
	}

	err = TLSkeyToFile(outDir+"/tls.key", tlsKey)
	if err != nil {
		log.Fatalln(err)
	}

	// set up our server certificate
	certTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2121),
		Subject: pkix.Name{
			Organization: []string{"NEIC"},
			CommonName:   "test_cert_1",
		},
		IPAddresses: []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
		DNSNames:    []string{"localhost"},
		NotBefore:   time.Now(),
		NotAfter:    time.Now().AddDate(0, 0, 1),
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		IsCA:        false,
	}
	hosts := strings.Split(*host, ",")
	certTemplate.DNSNames = append(certTemplate.DNSNames, hosts...)

	// create the TLS certificate
	certBytes, err := x509.CreateCertificate(rand.Reader, certTemplate, caTemplate, &tlsKey.PublicKey, caPrivKey)
	if err != nil {
		log.Fatalln(err)
	}

	err = TLScertToFile(outDir+"/tls.crt", certBytes)
	if err != nil {
		log.Fatalln(err)
	}
	log.Printf("certificartes written to: %s", outDir)
}

func TLSkeyToFile(filename string, key *ecdsa.PrivateKey) error {
	keyFile, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer keyFile.Close()

	pk, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return err
	}
	if err := pem.Encode(keyFile, &pem.Block{Type: "EC PRIVATE KEY", Bytes: pk}); err != nil {
		return err
	}

	return nil
}

func TLScertToFile(filename string, derBytes []byte) error {
	certFile, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer certFile.Close()
	if err := pem.Encode(certFile, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes}); err != nil {
		return err
	}

	return nil
}
