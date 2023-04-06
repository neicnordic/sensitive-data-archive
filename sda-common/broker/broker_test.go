package broker

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	log "github.com/sirupsen/logrus"
)

var tMqconf = MQConf{
	"127.0.0.1",
	5678,
	"user",
	"password",
	"/vhost",
	"queue",
	"exchange",
	"routingkey",
	"routingError",
	true,
	false,
	"../dev_utils/certs/ca.pem",
	"../dev_utils/certs/client.pem",
	"../dev_utils/certs/client-key.pem",
	"servername",
	true,
	"",
}

func TestBuildMqURI(t *testing.T) {
	amqps := buildMQURI("localhost", "user", "pass", "/vhost", 5555, true)
	assert.Equal(t, "amqps://user:pass@localhost:5555/vhost", amqps)
	amqp := buildMQURI("localhost", "user", "pass", "/vhost", 5555, false)
	assert.Equal(t, "amqp://user:pass@localhost:5555/vhost", amqp)
}

func TestTLSConfigBroker(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "gotest")
	assert.NoError(t, err)
	defer os.RemoveAll(tempDir)

	err = certsetup(tempDir)
	assert.NoError(t, err)

	confOK := tMqconf
	confOK.Ssl = true
	confOK.VerifyPeer = true
	confOK.CACert = tempDir + "/ca.crt"
	confOK.ClientCert = tempDir + "/tls.crt"
	confOK.ClientKey = tempDir + "/tls.key"

	tlsConfig, err := TLSConfigBroker(confOK)
	assert.NoError(t, err, "Unexpected error")
	assert.NotZero(t, tlsConfig.Certificates, "Expected warnings were missing")
	assert.NotZero(t, tlsConfig.RootCAs, "Expected warnings were missing")
	assert.EqualValues(t, tlsConfig.ServerName, "servername")

	noCa := confOK
	noCa.CACert = ""
	_, err = TLSConfigBroker(noCa)
	assert.NoError(t, err, "Unexpected error")

	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer func() {
		log.SetOutput(os.Stderr)
	}()
	noCa.CACert = tempDir + "/tls.key"
	_, err = TLSConfigBroker(noCa)
	assert.NoError(t, err, "Unexpected error")
	assert.Contains(t, buf.String(), "No certs appended, using system certs only")

	badCertConf := confOK
	badCertConf.ClientCert = tempDir + "/bar"
	_, err = CatchTLSConfigBrokerPanic(badCertConf)
	assert.EqualError(t, err, "open "+tempDir+"/bar: no such file or directory")

	badKeyConf := confOK
	badKeyConf.ClientKey = tempDir + "/foo"
	_, err = CatchTLSConfigBrokerPanic(badKeyConf)
	assert.EqualError(t, err, "open "+tempDir+"/foo: no such file or directory")

	noPemFile := confOK
	noPemFile.ClientKey = "broker.go"
	_, err = CatchTLSConfigBrokerPanic(noPemFile)
	assert.EqualError(t, err, "tls: failed to find any PEM data in key input")
}

func CatchTLSConfigBrokerPanic(c MQConf) (cfg *tls.Config, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("was panic, recovered value: %v", r)
		}
	}()

	cfg, err = TLSConfigBroker(c)

	return cfg, err
}

func TestNewMQNoTLS(t *testing.T) {
	noSslConf := tMqconf
	noSslConf.Ssl = false
	b, err := NewMQ(noSslConf)
	if err != nil {
		t.Log(err)
		t.Skip("skip test since a real MQ is not present")
	}
	assert.NotNil(t, b, "NewMQ without ssl did not return a broker")
	assert.False(t, b.Connection.IsClosed())

	b.Channel.Close()
	b.Connection.Close()
}

func TestNewMQTLS(t *testing.T) {
	certDir := fmt.Sprintf("/tmp/%d-%d-%d", time.Now().Year(), time.Now().Month(), time.Now().Day())

	SslConf := tMqconf
	SslConf.Port = 5679
	SslConf.CACert = certDir + "/ca.crt"
	SslConf.VerifyPeer = true
	SslConf.ClientCert = certDir + "/tls.crt"
	SslConf.ClientKey = certDir + "/tls.key"

	b, err := NewMQ(SslConf)
	if err != nil {
		t.Log(err)
		t.Skip("skip test since a real MQ is not present")
	}
	assert.NotNil(t, b, "NewMQ without ssl did not return a broker")
	assert.False(t, b.Connection.IsClosed())

	b.Channel.Close()
	b.Connection.Close()
}

func TestSendMessage(t *testing.T) {
	noSslConf := tMqconf
	noSslConf.Ssl = false
	b, err := NewMQ(noSslConf)
	if err != nil {
		t.Log(err)
		t.Skip("skip test since a real MQ is not present")
	}
	assert.NotNil(t, b, "NewMQ without ssl did not return a broker")
	assert.False(t, b.Connection.IsClosed())

	err = b.SendMessage("1", "", "queue", true, []byte("test message"))
	assert.NoError(t, err)

	b.Channel.Close()
	b.Connection.Close()
}

func TestGetMessages(t *testing.T) {
	noSslConf := tMqconf
	noSslConf.Ssl = false
	b, err := NewMQ(noSslConf)
	if err != nil {
		t.Log(err)
		t.Skip("skip test since a real MQ is not present")
	}
	assert.NotNil(t, b, "NewMQ without ssl did not return a broker")
	assert.False(t, b.Connection.IsClosed())

	d, err := b.GetMessages("queue")
	assert.NoError(t, err)

	for message := range d {
		if "test message" == string(message.Body) {
			err := message.Ack(false)
			assert.NoError(t, err)

			break
		}
	}

	b.Channel.Close()
	b.Connection.Close()
}

// Helper functions below this line

func certsetup(tempDir string) error {
	// set up our CA certificate
	ca := &x509.Certificate{
		SerialNumber: big.NewInt(2000),
		Subject: pkix.Name{
			Organization:  []string{"NEIC"},
			Country:       []string{""},
			Province:      []string{""},
			Locality:      []string{""},
			StreetAddress: []string{""},
			PostalCode:    []string{""},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().AddDate(0, 0, 7),
		IsCA:                  true,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}

	// create our private and public key
	caPrivKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}

	// create the CA certificate
	caBytes, err := x509.CreateCertificate(rand.Reader, ca, ca, &caPrivKey.PublicKey, caPrivKey)
	if err != nil {
		return err
	}

	err = TLScertToFile(tempDir+"/ca.crt", caBytes)
	if err != nil {
		return err
	}

	tlsKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}

	err = TLSkeyToFile(tempDir+"/tls.key", tlsKey)
	if err != nil {
		return err
	}

	// set up our server certificate
	cert := &x509.Certificate{
		SerialNumber: big.NewInt(2121),
		Subject: pkix.Name{
			Organization:  []string{"NEIC"},
			Country:       []string{""},
			Province:      []string{""},
			Locality:      []string{""},
			StreetAddress: []string{""},
			PostalCode:    []string{""},
		},
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
		DNSNames:     []string{"localhost", "servername"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().AddDate(0, 0, 1),
		SubjectKeyId: []byte{1, 2, 3, 4, 6},
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}

	// create the TLS certificate
	certBytes, err := x509.CreateCertificate(rand.Reader, cert, ca, &tlsKey.PublicKey, tlsKey)
	if err != nil {
		return err
	}

	err = TLScertToFile(tempDir+"/tls.crt", certBytes)

	return err
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

	return pem.Encode(keyFile, &pem.Block{Type: "EC PRIVATE KEY", Bytes: pk})
}

func TLScertToFile(filename string, derBytes []byte) error {
	certFile, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer certFile.Close()

	return pem.Encode(certFile, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
}
