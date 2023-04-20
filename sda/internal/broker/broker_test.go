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

	log "github.com/sirupsen/logrus"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type BrokerTestSuite struct {
	suite.Suite
}

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

func TestBrokerTestSuite(t *testing.T) {
	suite.Run(t, new(BrokerTestSuite))
}

func (suite *BrokerTestSuite) TestBuildMqURI() {
	amqps := buildMQURI("localhost", "user", "pass", "/vhost", 5555, true)
	assert.Equal(suite.T(), "amqps://user:pass@localhost:5555/vhost", amqps)
	amqp := buildMQURI("localhost", "user", "pass", "/vhost", 5555, false)
	assert.Equal(suite.T(), "amqp://user:pass@localhost:5555/vhost", amqp)
}

func (suite *BrokerTestSuite) TestTLSConfigBroker() {
	tempDir, err := os.MkdirTemp("", "gotest")
	assert.NoError(suite.T(), err)
	defer os.RemoveAll(tempDir)

	err = certsetup(tempDir)
	assert.NoError(suite.T(), err)

	confOK := tMqconf
	confOK.Ssl = true
	confOK.VerifyPeer = true
	confOK.CACert = tempDir + "/ca.crt"
	confOK.ClientCert = tempDir + "/tls.crt"
	confOK.ClientKey = tempDir + "/tls.key"

	tlsConfig, err := TLSConfigBroker(confOK)
	assert.NoError(suite.T(), err, "Unexpected error")
	assert.NotZero(suite.T(), tlsConfig.Certificates, "Expected warnings were missing")
	assert.NotZero(suite.T(), tlsConfig.RootCAs, "Expected warnings were missing")
	assert.EqualValues(suite.T(), tlsConfig.ServerName, "servername")

	noCa := confOK
	noCa.CACert = ""
	_, err = TLSConfigBroker(noCa)
	assert.NoError(suite.T(), err, "Unexpected error")

	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer func() {
		log.SetOutput(os.Stderr)
	}()
	noCa.CACert = tempDir + "/tls.key"
	_, err = TLSConfigBroker(noCa)
	assert.NoError(suite.T(), err, "Unexpected error")
	assert.Contains(suite.T(), buf.String(), "No certs appended, using system certs only")

	badCertConf := confOK
	badCertConf.ClientCert = tempDir + "/bar"
	_, err = CatchTLSConfigBrokerPanic(badCertConf)
	assert.EqualError(suite.T(), err, "open "+tempDir+"/bar: no such file or directory")

	badKeyConf := confOK
	badKeyConf.ClientKey = tempDir + "/foo"
	_, err = CatchTLSConfigBrokerPanic(badKeyConf)
	assert.EqualError(suite.T(), err, "open "+tempDir+"/foo: no such file or directory")

	noPemFile := confOK
	noPemFile.ClientKey = "broker.go"
	_, err = CatchTLSConfigBrokerPanic(noPemFile)
	assert.EqualError(suite.T(), err, "tls: failed to find any PEM data in key input")
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

func (suite *BrokerTestSuite) TestNewMQNoTLS() {
	noSslConf := tMqconf
	noSslConf.Ssl = false
	b, err := NewMQ(noSslConf)
	if err != nil {
		suite.T().Log(err)
		suite.T().Skip("skip test since a real MQ is not present")
	}
	assert.NotNil(suite.T(), b, "NewMQ without ssl did not return a broker")
	assert.False(suite.T(), b.Connection.IsClosed())

	b.Channel.Close()
	b.Connection.Close()
}

func (suite *BrokerTestSuite) TestNewMQTLS() {
	certDir := fmt.Sprintf("/tmp/%d-%d-%d", time.Now().Year(), time.Now().Month(), time.Now().Day())

	SslConf := tMqconf
	SslConf.Port = 5679
	SslConf.CACert = certDir + "/ca.crt"
	SslConf.VerifyPeer = true
	SslConf.ClientCert = certDir + "/tls.crt"
	SslConf.ClientKey = certDir + "/tls.key"

	b, err := NewMQ(SslConf)
	if err != nil {
		suite.T().Log(err)
		suite.T().Skip("skip test since a real MQ is not present")
	}
	assert.NotNil(suite.T(), b, "NewMQ without ssl did not return a broker")
	assert.False(suite.T(), b.Connection.IsClosed())

	b.Channel.Close()
	b.Connection.Close()
}

func (suite *BrokerTestSuite) TestSendMessage() {
	noSslConf := tMqconf
	noSslConf.Ssl = false
	b, err := NewMQ(noSslConf)
	if err != nil {
		suite.T().Log(err)
		suite.T().Skip("skip test since a real MQ is not present")
	}
	assert.NotNil(suite.T(), b, "NewMQ without ssl did not return a broker")
	assert.False(suite.T(), b.Connection.IsClosed())

	err = b.SendMessage("1", "", "queue", true, []byte("test message"))
	assert.NoError(suite.T(), err)

	b.Channel.Close()
	b.Connection.Close()
}

func (suite *BrokerTestSuite) TestGetMessages() {
	noSslConf := tMqconf
	noSslConf.Ssl = false
	b, err := NewMQ(noSslConf)
	if err != nil {
		suite.T().Log(err)
		suite.T().Skip("skip test since a real MQ is not present")
	}
	assert.NotNil(suite.T(), b, "NewMQ without ssl did not return a broker")
	assert.False(suite.T(), b.Connection.IsClosed())

	d, err := b.GetMessages("queue")
	assert.NoError(suite.T(), err)

	for message := range d {
		if "test message" == string(message.Body) {
			err := message.Ack(false)
			assert.NoError(suite.T(), err)

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
