package helper

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"log"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/golang-jwt/jwt/v4"
)

// Global variables for test token creation
var (
	DefaultTokenClaims = map[string]interface{}{
		"iss":   "https://dummy.ega.nbis.se",
		"sub":   "dummy",
		"exp":   time.Now().Add(time.Hour * 2).Unix(),
		"pilot": "dummy-pilot",
	}

	WrongUserClaims = map[string]interface{}{
		"sub":   "c5773f41d17d27bd53b1e6794aedc32d7906e779@elixir-europe.org",
		"aud":   "15137645-3153-4d49-9ddb-594027cd4ca7",
		"azp":   "15137645-3153-4d49-9ddb-594027cd4ca7",
		"scope": "ga4gh_passport_v1 openid",
		"iss":   "https://dummy.ega.nbis.se",
		"iat":   time.Now().Unix(),
		"exp":   time.Now().Add(time.Hour * 2).Unix(),
		"jti":   "5e6c6d24-42eb-408e-ba20-203904e388a1",
	}

	ExpiredClaims = map[string]interface{}{
		"sub":   "dummy",
		"aud":   "15137645-3153-4d49-9ddb-594027cd4ca7",
		"azp":   "15137645-3153-4d49-9ddb-594027cd4ca7",
		"scope": "ga4gh_passport_v1 openid",
		"iss":   "https://dummy.ega.nbis.se",
		"exp":   time.Now().Add(-time.Hour * 2).Unix(),
		"iat":   time.Now().Unix(),
		"jti":   "5e6c6d24-42eb-408e-ba20-203904e388a1",
	}

	NonValidClaims = map[string]interface{}{
		"sub":   "c5773f41d17d27bd53b1e6794aedc32d7906e779@elixir-europe.org",
		"aud":   "15137645-3153-4d49-9ddb-594027cd4ca7",
		"azp":   "15137645-3153-4d49-9ddb-594027cd4ca7",
		"scope": "ga4gh_passport_v1 openid",
		"iss":   "https://dummy.ega.nbis.se",
		"exp":   time.Now().Add(time.Hour * 2).Unix(),
		"iat":   time.Now().Add(time.Hour * 2).Unix(),
		"jti":   "5e6c6d24-42eb-408e-ba20-203904e388a1",
	}

	WrongTokenAlgClaims = map[string]interface{}{
		"iss":       "Online JWT Builder",
		"iat":       time.Now().Unix(),
		"exp":       time.Now().Add(time.Hour * 2).Unix(),
		"aud":       "4e9416a7-3515-447a-b848-d4ac7a57f",
		"sub":       "pleasefix@snurre-in-the-house.org",
		"auth_time": "1632207224",
		"jti":       "cc847f9c-7608-4b4f-9c6f-6e734813355f",
	}
)

// MakeFolder creates a folder and subfolders for the keys pair
// Returns the two paths
func MakeFolder(path string) (string, string, error) {
	prKeyPath := path + "/private-key"
	pubKeyPath := path + "/public-key"
	err := os.MkdirAll(prKeyPath, 0750)
	if err != nil {
		// fmt.Errorf("error creating directory: %v", err)
		return " ", " ", err
	}
	err = os.MkdirAll(pubKeyPath, 0750)
	if err != nil {
		// fmt.Errorf("error creatin directory: %w", err)
		return " ", " ", err
	}

	return prKeyPath, pubKeyPath, nil
}

// ParsePrivateRSAKey reads and parses the RSA private key
func ParsePrivateRSAKey(path, keyName string) (*rsa.PrivateKey, error) {
	keyPath := path + keyName
	prKey, err := os.ReadFile(filepath.Clean(keyPath))
	if err != nil {
		return nil, err
	}

	prKeyParsed, err := jwt.ParseRSAPrivateKeyFromPEM(prKey)
	if err != nil {
		return nil, err
	}

	return prKeyParsed, nil
}

// CreateRSAkeys creates the RSA key pair
func CreateRSAkeys(prPath, pubPath string) error {
	privatekey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}
	publickey := &privatekey.PublicKey

	// dump private key to file
	privateKeyBytes := x509.MarshalPKCS1PrivateKey(privatekey)
	privateKeyBlock := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: privateKeyBytes,
	}
	privatePem, err := os.Create(prPath + "/dummy.ega.nbis.se")
	if err != nil {
		return err
	}
	err = pem.Encode(privatePem, privateKeyBlock)
	if err != nil {
		return err
	}

	// dump public key to file
	publicKeyBytes, err := x509.MarshalPKIXPublicKey(publickey)
	if err != nil {
		return err
	}
	publicKeyBlock := &pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: publicKeyBytes,
	}
	publicPem, err := os.Create(pubPath + "/dummy.ega.nbis.se.pub")
	if err != nil {
		return err
	}
	err = pem.Encode(publicPem, publicKeyBlock)
	if err != nil {
		return err
	}

	return nil
}

// CreateRSAToken creates an RSA token
func CreateRSAToken(key *rsa.PrivateKey, headerAlg, headerType string, tokenClaims map[string]interface{}) (string, error) {
	token := jwt.New(jwt.SigningMethodRS256)
	token.Header["alg"] = headerAlg
	token.Header["typ"] = headerType
	claims := make(jwt.MapClaims)
	for key, value := range tokenClaims {
		claims[key] = value
	}
	token.Claims = claims
	tokenString, err := token.SignedString(key)
	if err != nil {
		return "no-token", err
	}

	return tokenString, nil
}

// CreateECToken creates an EC token
func CreateECToken(key *ecdsa.PrivateKey, headerAlg, headerType string, tokenClaims map[string]interface{}) (string, error) {
	token := jwt.New(jwt.SigningMethodES256)
	token.Header["alg"] = headerAlg
	token.Header["typ"] = headerType
	claims := make(jwt.MapClaims)
	for key, value := range tokenClaims {
		claims[key] = value
	}
	token.Claims = claims
	tokenString, err := token.SignedString(key)
	if err != nil {
		return "no-token", err
	}

	return tokenString, nil
}

// CreateHSToken creates an HS token
func CreateHSToken(key []byte, headerAlg, headerType string, tokenClaims map[string]interface{}) (string, error) {
	token := jwt.New(jwt.SigningMethodHS256)
	token.Header["alg"] = headerAlg
	token.Header["typ"] = headerType
	claims := make(jwt.MapClaims)
	for key, value := range tokenClaims {
		claims[key] = value
	}
	token.Claims = claims
	tokenString, err := token.SignedString(key)
	if err != nil {
		return "no-token", err
	}

	return tokenString, nil
}

// ParsePrivateECKey reads and parses the EC private key
func ParsePrivateECKey(path, keyName string) (*ecdsa.PrivateKey, error) {
	keyPath := path + keyName
	prKey, err := os.ReadFile(filepath.Clean(keyPath))
	if err != nil {
		return nil, err
	}

	prKeyParsed, err := jwt.ParseECPrivateKeyFromPEM(prKey)
	if err != nil {
		return nil, err
	}

	return prKeyParsed, nil
}

// CreateECkeys creates the EC key pair
func CreateECkeys(prPath, pubPath string) error {
	privatekey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	publickey := &privatekey.PublicKey

	// dump private key to file
	privateKeyBytes, _ := x509.MarshalECPrivateKey(privatekey)
	privateKeyBlock := &pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: privateKeyBytes,
	}
	privatePem, err := os.Create(prPath + "/dummy.ega.nbis.se")
	if err != nil {
		return err
	}
	err = pem.Encode(privatePem, privateKeyBlock)
	if err != nil {
		return err
	}

	// dump public key to file
	publicKeyBytes, err := x509.MarshalPKIXPublicKey(publickey)
	if err != nil {
		return err
	}
	publicKeyBlock := &pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: publicKeyBytes,
	}
	publicPem, err := os.Create(pubPath + "/dummy.ega.nbis.se.pub")
	if err != nil {
		return err
	}
	err = pem.Encode(publicPem, publicKeyBlock)
	if err != nil {
		return err
	}

	return nil
}

func MakeCerts(outDir string) {

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
			CommonName:   "test_cert",
		},
		IPAddresses: []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
		DNSNames:    []string{"localhost,mq,proxy,s3"},
		NotBefore:   time.Now(),
		NotAfter:    time.Now().AddDate(0, 0, 1),
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		IsCA:        false,
	}

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
