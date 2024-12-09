package config

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/neicnordic/crypt4gh/keys"
	"github.com/neicnordic/sensitive-data-archive/internal/broker"
	"github.com/neicnordic/sensitive-data-archive/internal/database"
	"github.com/neicnordic/sensitive-data-archive/internal/storage"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

const (
	POSIX = "posix"
	S3    = "s3"
	SFTP  = "sftp"
)

var requiredConfVars []string

// ServerConfig stores general server information
type ServerConfig struct {
	Cert          string
	Key           string
	Jwtpubkeypath string
	Jwtpubkeyurl  string
	CORS          CORSConfig
}

// Config is a parent object for all the different configuration parts
type Config struct {
	Archive      storage.Conf
	Broker       broker.MQConf
	Database     database.DBConf
	Inbox        storage.Conf
	Backup       storage.Conf
	Server       ServerConfig
	API          APIConf
	Notify       SMTPConf
	Orchestrator OrchestratorConf
	Sync         Sync
	SyncAPI      SyncAPIConf
	ReEncrypt    ReEncConfig
	Auth         AuthConf
}

type ReEncConfig struct {
	APIConf
	Crypt4GHKey *[32]byte
}

type Sync struct {
	CenterPrefix   string
	Destination    storage.Conf
	RemoteHost     string
	RemotePassword string
	RemotePort     int
	RemoteUser     string
}

type SyncAPIConf struct {
	APIPassword      string
	APIUser          string
	AccessionRouting string `default:"accession"`
	IngestRouting    string `default:"ingest"`
	MappingRouting   string `default:"mappings"`
}

type APIConf struct {
	RBACpolicy []byte
	CACert     string
	ServerCert string
	ServerKey  string
	Host       string
	Port       int
	Session    SessionConfig
	DB         *database.SDAdb
	MQ         *broker.AMQPBroker
	INBOX      storage.Backend
}

type SessionConfig struct {
	Expiration time.Duration
	Domain     string
	Secure     bool
	HTTPOnly   bool
	Name       string
}

type SMTPConf struct {
	Password string
	FromAddr string
	Host     string
	Port     int
}

type OrchestratorConf struct {
	ProjectFQDN    string
	QueueVerify    string
	QueueInbox     string
	QueueComplete  string
	QueueBackup    string
	QueueMapping   string
	QueueIngest    string
	QueueAccession string
	ReleaseDelay   time.Duration
}

type AuthConf struct {
	OIDC            OIDCConfig
	DB              *database.SDAdb
	Cega            CegaConfig
	JwtIssuer       string
	JwtPrivateKey   string
	JwtSignatureAlg string
	JwtTTL          int
	Server          ServerConfig
	S3Inbox         string
	ResignJwt       bool
	InfoURL         string
	InfoText        string
	PublicFile      string
}

type OIDCConfig struct {
	ID            string
	Provider      string
	RedirectURL   string
	RevocationURL string
	Secret        string
	JwkURL        string
}

type CegaConfig struct {
	AuthURL string
	ID      string
	Secret  string
}

type CORSConfig struct {
	AllowOrigin      string
	AllowMethods     string
	AllowHeaders     string
	AllowCredentials bool
}

// NewConfig initializes and parses the config file and/or environment using
// the viper library.
func NewConfig(app string) (*Config, error) {
	viper.SetConfigName("config")
	viper.AddConfigPath(".")
	viper.AutomaticEnv()
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.SetConfigType("yaml")
	viper.SetDefault("schema.type", "federated")

	if viper.IsSet("configPath") {
		cp := viper.GetString("configPath")
		log.Infof("configPath: %s", cp)
		if !strings.HasSuffix(cp, "/") {
			cp += "/"
		}
		viper.AddConfigPath(cp)
	}
	if viper.IsSet("configFile") {
		viper.SetConfigFile(viper.GetString("configFile"))
	}
	log.Infoln("reading config")
	if err := viper.ReadInConfig(); err != nil {
		log.Infoln(err.Error())
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			log.Infoln("No config file found, using ENVs only")
		} else {
			log.Infoln("ReadInConfig Error")

			return nil, err
		}
	}

	if viper.IsSet("log.format") {
		if viper.GetString("log.format") == "json" {
			log.SetFormatter(&log.JSONFormatter{})
			log.Info("The logs format is set to JSON")
		}
	}

	if viper.IsSet("log.level") {
		stringLevel := viper.GetString("log.level")
		intLevel, err := log.ParseLevel(stringLevel)
		if err != nil {
			log.Infof("Log level '%s' not supported, setting to 'trace'", stringLevel)
			intLevel = log.TraceLevel
		}
		log.SetLevel(intLevel)
		log.Infof("Setting log level to '%s'", stringLevel)
	}

	switch app {
	case "api":
		requiredConfVars = []string{
			"api.rbacFile",
			"broker.host",
			"broker.port",
			"broker.user",
			"broker.password",
			"db.host",
			"db.port",
			"db.user",
			"db.password",
			"db.database",
		}
		switch viper.GetString("inbox.type") {
		case S3:
			requiredConfVars = append(requiredConfVars, []string{"inbox.url", "inbox.accesskey", "inbox.secretkey", "inbox.bucket"}...)
		case POSIX:
			requiredConfVars = append(requiredConfVars, []string{"inbox.location"}...)
		default:
			return nil, fmt.Errorf("inbox.type not set")
		}
	case "auth":
		requiredConfVars = []string{
			"auth.s3Inbox",
			"auth.publicFile",
			"db.host",
			"db.port",
			"db.user",
			"db.password",
			"db.database",
		}

		if viper.GetString("auth.cega.id") != "" && viper.GetString("auth.cega.secret") != "" {
			requiredConfVars = append(requiredConfVars, []string{"auth.cega.authUrl"}...)
			viper.Set("auth.resignJwt", true)
		}

		if viper.GetString("oidc.id") != "" && viper.GetString("oidc.secret") != "" {
			requiredConfVars = append(requiredConfVars, []string{"oidc.provider", "oidc.redirectUrl"}...)
		}

		if viper.GetBool("auth.resignJwt") {
			requiredConfVars = append(requiredConfVars, []string{"auth.jwt.issuer", "auth.jwt.privateKey", "auth.jwt.signatureAlg", "auth.jwt.tokenTTL"}...)
		}
	case "ingest":
		requiredConfVars = []string{
			"broker.host",
			"broker.port",
			"broker.user",
			"broker.password",
			"broker.queue",
			"broker.routingkey",
			"db.host",
			"db.port",
			"db.user",
			"db.password",
			"db.database",
		}

		switch viper.GetString("archive.type") {
		case S3:
			requiredConfVars = append(requiredConfVars, []string{"archive.url", "archive.accesskey", "archive.secretkey", "archive.bucket"}...)
		case POSIX:
			requiredConfVars = append(requiredConfVars, []string{"archive.location"}...)
		default:
			return nil, fmt.Errorf("archive.type not set")
		}

		switch viper.GetString("inbox.type") {
		case S3:
			requiredConfVars = append(requiredConfVars, []string{"inbox.url", "inbox.accesskey", "inbox.secretkey", "inbox.bucket"}...)
		case POSIX:
			requiredConfVars = append(requiredConfVars, []string{"inbox.location"}...)
		default:
			return nil, fmt.Errorf("inbox.type not set")
		}
	case "finalize":
		requiredConfVars = []string{
			"broker.host",
			"broker.port",
			"broker.user",
			"broker.password",
			"broker.queue",
			"broker.routingkey",
			"db.host",
			"db.port",
			"db.user",
			"db.password",
			"db.database",
		}

		switch viper.GetString("archive.type") {
		case S3:
			requiredConfVars = append(requiredConfVars, []string{"archive.url", "archive.accesskey", "archive.secretkey", "archive.bucket"}...)
		case POSIX:
			requiredConfVars = append(requiredConfVars, []string{"archive.location"}...)
		}

		switch viper.GetString("backup.type") {
		case S3:
			requiredConfVars = append(requiredConfVars, []string{"backup.url", "backup.accesskey", "backup.secretkey", "backup.bucket"}...)
		case POSIX:
			requiredConfVars = append(requiredConfVars, []string{"backup.location"}...)
		}
	case "intercept":
		requiredConfVars = []string{
			"broker.host",
			"broker.port",
			"broker.user",
			"broker.password",
			"broker.queue",
		}
	case "mapper":
		// Mapper does not require broker.routingkey thus we remove it
		requiredConfVars = []string{
			"broker.host",
			"broker.port",
			"broker.user",
			"broker.password",
			"broker.queue",
			"db.host",
			"db.port",
			"db.user",
			"db.password",
			"db.database",
		}

		switch viper.GetString("inbox.type") {
		case S3:
			requiredConfVars = append(requiredConfVars, []string{"inbox.url", "inbox.accesskey", "inbox.secretkey", "inbox.bucket"}...)
		case POSIX:
			requiredConfVars = append(requiredConfVars, []string{"inbox.location"}...)
		}
	case "notify":
		requiredConfVars = []string{
			"broker.host",
			"broker.port",
			"broker.user",
			"broker.password",
			"broker.queue",
			"smtp.host",
			"smtp.port",
			"smtp.password",
			"smtp.from",
		}
	case "orchestrate":
		// Orchestrate requires broker connection, a series of
		// queues, and the project FQDN.
		requiredConfVars = []string{
			"broker.host",
			"broker.port",
			"broker.user",
			"broker.password",
			"project.fqdn",
		}
	case "reencrypt":
		requiredConfVars = []string{
			"c4gh.filepath",
			"c4gh.passphrase",
		}
	case "s3inbox":
		requiredConfVars = []string{
			"broker.host",
			"broker.port",
			"broker.user",
			"broker.password",
			"broker.routingkey",
			"inbox.url",
			"inbox.accesskey",
			"inbox.secretkey",
			"inbox.bucket",
		}
		viper.Set("inbox.type", S3)
	case "sync":
		requiredConfVars = []string{
			"broker.host",
			"broker.port",
			"broker.user",
			"broker.password",
			"broker.queue",
			"c4gh.filepath",
			"c4gh.passphrase",
			"c4gh.syncPubKeyPath",
			"db.host",
			"db.port",
			"db.user",
			"db.password",
			"db.database",
			"sync.centerPrefix",
			"sync.remote.host",
			"sync.remote.user",
			"sync.remote.password",
		}

		switch viper.GetString("archive.type") {
		case S3:
			requiredConfVars = append(requiredConfVars, []string{"archive.url", "archive.accesskey", "archive.secretkey", "archive.bucket"}...)
		case POSIX:
			requiredConfVars = append(requiredConfVars, []string{"archive.location"}...)
		default:
			return nil, fmt.Errorf("archive.type not set")
		}

		switch viper.GetString("sync.destination.type") {
		case S3:
			requiredConfVars = append(requiredConfVars, []string{"sync.destination.url", "sync.destination.accesskey", "sync.destination.secretkey", "sync.destination.bucket"}...)
		case POSIX:
			requiredConfVars = append(requiredConfVars, []string{"sync.destination.location"}...)
		case SFTP:
			requiredConfVars = append(requiredConfVars, []string{"sync.destination.sftp.host", "sync.destination.sftp.port", "sync.destination.sftp.userName", "sync.destination.sftp.pemKeyPath", "sync.destination.sftp.pemKeyPass"}...)
		default:
			return nil, fmt.Errorf("sync.destination.type not set")
		}
	case "sync-api":
		requiredConfVars = []string{
			"broker.exchange",
			"broker.host",
			"broker.port",
			"broker.user",
			"broker.password",
			"sync.api.user",
			"sync.api.password",
		}
	case "verify":
		requiredConfVars = []string{
			"broker.host",
			"broker.port",
			"broker.user",
			"broker.password",
			"broker.queue",
			"broker.routingkey",
			"db.host",
			"db.port",
			"db.user",
			"db.password",
			"db.database",
		}

		switch viper.GetString("archive.type") {
		case S3:
			requiredConfVars = append(requiredConfVars, []string{"archive.url", "archive.accesskey", "archive.secretkey", "archive.bucket"}...)
		case POSIX:
			requiredConfVars = append(requiredConfVars, []string{"archive.location"}...)
		default:
			return nil, fmt.Errorf("archive.type not set")
		}

	default:
		return nil, fmt.Errorf("application '%s' doesn't exist", app)
	}

	for _, s := range requiredConfVars {
		if !viper.IsSet(s) {
			return nil, fmt.Errorf("%s not set", s)
		}
	}

	c := &Config{}
	switch app {
	case "api":
		err := c.configBroker()
		if err != nil {
			return nil, err
		}

		err = c.configDatabase()
		if err != nil {
			return nil, err
		}

		c.configInbox()

		err = c.configAPI()
		if err != nil {
			return nil, err
		}

		err = c.configServer()
		if err != nil {
			return nil, err
		}
		c.API.RBACpolicy, err = os.ReadFile(viper.GetString("api.rbacFile"))
		if err != nil {
			return nil, err
		}
		c.configSchemas()
	case "auth":
		c.Auth.Cega.AuthURL = viper.GetString("auth.cega.authUrl")
		c.Auth.Cega.ID = viper.GetString("auth.cega.id")
		c.Auth.Cega.Secret = viper.GetString("auth.cega.secret")

		c.Auth.OIDC.ID = viper.GetString("oidc.id")
		c.Auth.OIDC.Provider = viper.GetString("oidc.provider")
		c.Auth.OIDC.RedirectURL = viper.GetString("oidc.redirectUrl")
		c.Auth.OIDC.Secret = viper.GetString("oidc.secret")
		if viper.IsSet("oidc.jwkPath") {
			c.Auth.OIDC.JwkURL = c.Auth.OIDC.Provider + viper.GetString("oidc.jwkPath")
		}

		if (c.Auth.OIDC.ID == "" || c.Auth.OIDC.Secret == "") && (c.Auth.Cega.ID == "" || c.Auth.Cega.Secret == "") {
			return nil, fmt.Errorf("neither cega or oidc login configured")
		}

		c.Auth.InfoURL = viper.GetString("auth.infoUrl")
		c.Auth.InfoText = viper.GetString("auth.infoText")
		c.Auth.PublicFile = viper.GetString("auth.publicFile")
		if _, err := os.Stat(c.Auth.PublicFile); err != nil {
			return nil, err
		}

		if viper.GetBool("auth.resignJwt") {
			c.Auth.ResignJwt = viper.GetBool("auth.resignJwt")
			c.Auth.JwtPrivateKey = viper.GetString("auth.jwt.privateKey")
			c.Auth.JwtSignatureAlg = viper.GetString("auth.jwt.signatureAlg")
			c.Auth.JwtIssuer = viper.GetString("auth.jwt.issuer")
			c.Auth.JwtTTL = viper.GetInt("auth.jwt.tokenTTL")

			if _, err := os.Stat(c.Auth.JwtPrivateKey); err != nil {
				return nil, err
			}
		}

		cors := CORSConfig{AllowCredentials: false}
		if viper.IsSet("cors.origins") {
			cors.AllowOrigin = viper.GetString("cors.origins")
		}
		if viper.IsSet("cors.methods") {
			cors.AllowMethods = viper.GetString("cors.methods")
		}
		if viper.IsSet("cors.headers") {
			cors.AllowHeaders = viper.GetString("cors.headers")
		}
		if viper.IsSet("cors.credentials") {
			cors.AllowCredentials = viper.GetBool("cors.credentials")
		}
		c.Server.CORS = cors

		if viper.IsSet("server.cert") {
			c.Server.Cert = viper.GetString("server.cert")
		}
		if viper.IsSet("server.key") {
			c.Server.Key = viper.GetString("server.key")
		}

		c.Auth.S3Inbox = viper.GetString("auth.s3Inbox")
		err := c.configDatabase()
		if err != nil {
			return nil, err
		}
	case "finalize":
		if viper.GetString("archive.type") != "" && viper.GetString("backup.type") != "" {
			c.configArchive()
			c.configBackup()
		}

		err := c.configBroker()
		if err != nil {
			return nil, err
		}
		err = c.configDatabase()
		if err != nil {
			return nil, err
		}

		c.configSchemas()
	case "ingest":
		c.configArchive()
		err := c.configBroker()
		if err != nil {
			return nil, err
		}
		err = c.configDatabase()
		if err != nil {
			return nil, err
		}

		c.configInbox()
		c.configSchemas()
	case "intercept":
		err := c.configBroker()
		if err != nil {
			return nil, err
		}

		c.configSchemas()
	case "mapper":
		err := c.configBroker()
		if err != nil {
			return nil, err
		}

		err = c.configDatabase()
		if err != nil {
			return nil, err
		}

		c.configInbox()
		c.configSchemas()
	case "notify":
		c.configSMTP()

		return c, nil
	case "orchestrate":
		err := c.configBroker()
		if err != nil {
			return nil, err
		}

		c.configOrchestrator()
	case "reencrypt":
		viper.SetDefault("grpc.host", "0.0.0.0")
		viper.SetDefault("grpc.port", 50051)
		err := c.configReEncryptServer()
		if err != nil {
			return nil, err
		}
	case "s3inbox":
		err := c.configBroker()
		if err != nil {
			return nil, err
		}

		err = c.configDatabase()
		if err != nil {
			return nil, err
		}

		c.configInbox()

		err = c.configServer()
		if err != nil {
			return nil, err
		}
	case "sync":
		if err := c.configBroker(); err != nil {
			return nil, err
		}

		if err := c.configDatabase(); err != nil {
			return nil, err
		}

		c.configArchive()
		c.configSync()
		c.configSchemas()
	case "sync-api":
		if err := c.configBroker(); err != nil {
			return nil, err
		}

		if err := c.configAPI(); err != nil {
			return nil, err
		}

		c.configSyncAPI()
		c.configSchemas()
	case "verify":
		c.configArchive()

		err := c.configBroker()
		if err != nil {
			return nil, err
		}

		err = c.configDatabase()
		if err != nil {
			return nil, err
		}

		c.configSchemas()
	}

	return c, nil
}

// configDatabase provides configuration for the database
func (c *Config) configAPI() error {
	c.apiDefaults()
	api := APIConf{}

	api.Session.Expiration = time.Duration(viper.GetInt("api.session.expiration")) * time.Second
	api.Session.Domain = viper.GetString("api.session.domain")
	api.Session.Secure = viper.GetBool("api.session.secure")
	api.Session.HTTPOnly = viper.GetBool("api.session.httponly")
	api.Session.Name = viper.GetString("api.session.name")

	api.Host = viper.GetString("api.host")
	api.Port = viper.GetInt("api.port")
	api.ServerKey = viper.GetString("api.serverKey")
	api.ServerCert = viper.GetString("api.serverCert")
	api.CACert = viper.GetString("api.CACert")

	c.API = api

	return nil
}

// apiDefaults set default values for web server and session
func (c *Config) apiDefaults() {
	viper.SetDefault("api.host", "0.0.0.0")
	viper.SetDefault("api.port", 8080)
	viper.SetDefault("api.session.expiration", -1)
	viper.SetDefault("api.session.secure", true)
	viper.SetDefault("api.session.httponly", true)
	viper.SetDefault("api.session.name", "api_session_key")
}

// configArchive provides configuration for the archive storage
func (c *Config) configArchive() {
	if viper.GetString("archive.type") == S3 {
		c.Archive.Type = S3
		c.Archive.S3 = configS3Storage("archive")
	} else {
		c.Archive.Type = POSIX
		c.Archive.Posix.Location = viper.GetString("archive.location")
	}
}

// configBackup provides configuration for the backup storage
func (c *Config) configBackup() {
	switch viper.GetString("backup.type") {
	case S3:
		c.Backup.Type = S3
		c.Backup.S3 = configS3Storage("backup")
	case SFTP:
		c.Backup.Type = SFTP
		c.Backup.SFTP = configSFTP("backup")
	default:
		c.Backup.Type = POSIX
		c.Backup.Posix.Location = viper.GetString("backup.location")
	}
}

// configBroker provides configuration for the message broker
func (c *Config) configBroker() error {
	// Setup broker
	broker := broker.MQConf{}

	broker.Host = viper.GetString("broker.host")
	broker.Port = viper.GetInt("broker.port")
	broker.User = viper.GetString("broker.user")
	broker.Password = viper.GetString("broker.password")

	broker.Queue = viper.GetString("broker.queue")

	if viper.IsSet("broker.serverName") {
		broker.ServerName = viper.GetString("broker.serverName")
	}

	if viper.IsSet("broker.routingkey") {
		broker.RoutingKey = viper.GetString("broker.routingkey")
	}

	if viper.IsSet("broker.exchange") {
		broker.Exchange = viper.GetString("broker.exchange")
	}

	if viper.IsSet("broker.vhost") {
		if strings.HasPrefix(viper.GetString("broker.vhost"), "/") {
			broker.Vhost = viper.GetString("broker.vhost")
		} else {
			broker.Vhost = "/" + viper.GetString("broker.vhost")
		}
	} else {
		broker.Vhost = "/"
	}

	if viper.IsSet("broker.ssl") {
		broker.Ssl = viper.GetBool("broker.ssl")
	}

	if viper.IsSet("broker.verifyPeer") {
		broker.VerifyPeer = viper.GetBool("broker.verifyPeer")
		if broker.VerifyPeer {
			// Since verifyPeer is specified, these are required.
			if !(viper.IsSet("broker.clientCert") && viper.IsSet("broker.clientKey")) {
				return errors.New("when broker.verifyPeer is set both broker.clientCert and broker.clientKey is needed")
			}
			broker.ClientCert = viper.GetString("broker.clientCert")
			broker.ClientKey = viper.GetString("broker.clientKey")
		}
	}
	if viper.IsSet("broker.cacert") {
		broker.CACert = viper.GetString("broker.cacert")
	}

	broker.PrefetchCount = 2
	if viper.IsSet("broker.prefetchCount") {
		broker.PrefetchCount = viper.GetInt("broker.prefetchCount")
	}

	c.Broker = broker

	return nil
}

// configDatabase provides configuration for the database
func (c *Config) configDatabase() error {
	db := database.DBConf{}

	// All these are required
	db.Host = viper.GetString("db.host")
	db.Port = viper.GetInt("db.port")
	db.User = viper.GetString("db.user")
	db.Password = viper.GetString("db.password")
	db.Database = viper.GetString("db.database")
	db.SslMode = viper.GetString("db.sslmode")

	// Optional settings
	if db.SslMode == "verify-full" {
		// Since verify-full is specified, these are required.
		if !(viper.IsSet("db.clientCert") && viper.IsSet("db.clientKey")) {
			return errors.New("when db.sslMode is set to verify-full both db.clientCert and db.clientKey are needed")
		}
	}
	if viper.IsSet("db.clientKey") {
		db.ClientKey = viper.GetString("db.clientKey")
	}
	if viper.IsSet("db.clientCert") {
		db.ClientCert = viper.GetString("db.clientCert")
	}
	if viper.IsSet("db.cacert") {
		db.CACert = viper.GetString("db.cacert")
	}

	c.Database = db

	return nil
}

// configInbox provides configuration for the inbox storage
func (c *Config) configInbox() {
	if viper.GetString("inbox.type") == S3 {
		c.Inbox.Type = S3
		c.Inbox.S3 = configS3Storage("inbox")
	} else {
		c.Inbox.Type = POSIX
		c.Inbox.Posix.Location = viper.GetString("inbox.location")
	}
}

// configOrchestrator provides the configuration for the standalone orchestator.
func (c *Config) configOrchestrator() {
	c.Orchestrator = OrchestratorConf{}
	if viper.IsSet("broker.dataset.releasedelay") {
		c.Orchestrator.ReleaseDelay = time.Duration(viper.GetInt("broker.dataset.releasedelay"))
	} else {
		c.Orchestrator.ReleaseDelay = 1
	}
	c.Orchestrator.ProjectFQDN = viper.GetString("project.fqdn")
	if viper.IsSet("broker.queue.verified") {
		c.Orchestrator.QueueVerify = viper.GetString("broker.queue.verified")
	} else {
		c.Orchestrator.QueueVerify = "verified"
	}

	if viper.IsSet("broker.queue.inbox") {
		c.Orchestrator.QueueInbox = viper.GetString("broker.queue.inbox")
	} else {
		c.Orchestrator.QueueInbox = "inbox"
	}

	if viper.IsSet("broker.queue.completed") {
		c.Orchestrator.QueueComplete = viper.GetString("broker.queue.completed")
	} else {
		c.Orchestrator.QueueComplete = "completed"
	}

	if viper.IsSet("broker.queue.backup") {
		c.Orchestrator.QueueBackup = viper.GetString("broker.queue.backup")
	} else {
		c.Orchestrator.QueueBackup = "backup"
	}

	if viper.IsSet("broker.queue.mappings") {
		c.Orchestrator.QueueMapping = viper.GetString("broker.queue.mappings")
	} else {
		c.Orchestrator.QueueMapping = "mappings"
	}

	if viper.IsSet("broker.queue.ingest") {
		c.Orchestrator.QueueIngest = viper.GetString("broker.queue.ingest")
	} else {
		c.Orchestrator.QueueIngest = "ingest"
	}

	if viper.IsSet("broker.queue.accessionIDs") {
		c.Orchestrator.QueueAccession = viper.GetString("broker.queue.accessionIDs")
	} else {
		c.Orchestrator.QueueAccession = "accessionIDs"
	}
}

func (c *Config) configReEncryptServer() (err error) {
	if viper.IsSet("grpc.host") {
		c.ReEncrypt.Host = viper.GetString("grpc.host")
	}
	if viper.IsSet("grpc.port") {
		c.ReEncrypt.Port = viper.GetInt("grpc.port")
	}
	if viper.IsSet("grpc.cacert") {
		c.ReEncrypt.CACert = viper.GetString("grpc.cacert")
	}
	if viper.IsSet("grpc.servercert") {
		c.ReEncrypt.ServerCert = viper.GetString("grpc.servercert")
	}
	if viper.IsSet("grpc.serverkey") {
		c.ReEncrypt.ServerKey = viper.GetString("grpc.serverkey")
	}

	if c.ReEncrypt.ServerCert != "" && c.ReEncrypt.ServerKey != "" {
		c.ReEncrypt.Port = 50443
	}

	c.ReEncrypt.Crypt4GHKey, err = GetC4GHKey()
	if err != nil {
		return err
	}

	return nil
}

// configSchemas configures the schemas to load depending on
// the type IDs of connection Federated EGA or isolate (stand-alone)
func (c *Config) configSchemas() {
	if viper.GetString("schema.type") == "federated" {
		c.Broker.SchemasPath = "/schemas/federated/"
	} else {
		c.Broker.SchemasPath = "/schemas/isolated/"
	}
}

// configS3Storage populates and returns a S3Conf from the
// configuration
func configS3Storage(prefix string) storage.S3Conf {
	s3 := storage.S3Conf{}
	// All these are required
	s3.URL = viper.GetString(prefix + ".url")
	s3.AccessKey = viper.GetString(prefix + ".accesskey")
	s3.SecretKey = viper.GetString(prefix + ".secretkey")
	s3.Bucket = viper.GetString(prefix + ".bucket")

	// Defaults (move to viper?)

	s3.Region = "us-east-1"
	s3.NonExistRetryTime = 2 * time.Minute

	if viper.IsSet(prefix + ".port") {
		s3.Port = viper.GetInt(prefix + ".port")
	}

	if viper.IsSet(prefix + ".region") {
		s3.Region = viper.GetString(prefix + ".region")
	}

	if viper.IsSet(prefix + ".readypath") {
		s3.Readypath = viper.GetString(prefix + ".readypath")
	}

	if viper.IsSet(prefix + ".chunksize") {
		s3.Chunksize = viper.GetInt(prefix+".chunksize") * 1024 * 1024
	}

	if viper.IsSet(prefix + ".cacert") {
		s3.CAcert = viper.GetString(prefix + ".cacert")
	}

	return s3
}

// configSFTP populates and returns a sftpConf with sftp backend configuration
func configSFTP(prefix string) storage.SftpConf {
	sftpConf := storage.SftpConf{}
	if viper.IsSet(prefix + ".sftp.hostKey") {
		sftpConf.HostKey = viper.GetString(prefix + ".sftp.hostKey")
	} else {
		sftpConf.HostKey = ""
	}
	// All these are required
	sftpConf.Host = viper.GetString(prefix + ".sftp.host")
	sftpConf.Port = viper.GetString(prefix + ".sftp.port")
	sftpConf.UserName = viper.GetString(prefix + ".sftp.userName")
	sftpConf.PemKeyPath = viper.GetString(prefix + ".sftp.pemKeyPath")
	sftpConf.PemKeyPass = viper.GetString(prefix + ".sftp.pemKeyPass")

	return sftpConf
}

// configNotify provides configuration for the backup storage
func (c *Config) configSMTP() {
	c.Notify = SMTPConf{}
	c.Notify.Host = viper.GetString("smtp.host")
	c.Notify.Port = viper.GetInt("smtp.port")
	c.Notify.Password = viper.GetString("smtp.password")
	c.Notify.FromAddr = viper.GetString("smtp.from")
}

// configSync provides configuration for the sync destination storage
func (c *Config) configSync() {
	switch viper.GetString("sync.destination.type") {
	case S3:
		c.Sync.Destination.Type = S3
		c.Sync.Destination.S3 = configS3Storage("sync.destination")
	case SFTP:
		c.Sync.Destination.Type = SFTP
		c.Sync.Destination.SFTP = configSFTP("sync.destination")
	case POSIX:
		c.Sync.Destination.Type = POSIX
		c.Sync.Destination.Posix.Location = viper.GetString("sync.destination.location")
	}

	c.Sync.RemoteHost = viper.GetString("sync.remote.host")
	if viper.IsSet("sync.remote.port") {
		c.Sync.RemotePort = viper.GetInt("sync.remote.port")
	}
	c.Sync.RemotePassword = viper.GetString("sync.remote.password")
	c.Sync.RemoteUser = viper.GetString("sync.remote.user")
	c.Sync.CenterPrefix = viper.GetString("sync.centerPrefix")
}

// configSync provides configuration for the outgoing sync settings
func (c *Config) configSyncAPI() {
	c.SyncAPI = SyncAPIConf{}
	c.SyncAPI.APIPassword = viper.GetString("sync.api.password")
	c.SyncAPI.APIUser = viper.GetString("sync.api.user")

	if viper.IsSet("sync.api.AccessionRouting") {
		c.SyncAPI.AccessionRouting = viper.GetString("sync.api.AccessionRouting")
	}
	if viper.IsSet("sync.api.IngestRouting") {
		c.SyncAPI.IngestRouting = viper.GetString("sync.api.IngestRouting")
	}
	if viper.IsSet("sync.api.MappingRouting") {
		c.SyncAPI.MappingRouting = viper.GetString("sync.api.MappingRouting")
	}

}

// GetC4GHKey reads and decrypts and returns the c4gh key
func GetC4GHKey() (*[32]byte, error) {
	keyPath := viper.GetString("c4gh.filepath")
	passphrase := viper.GetString("c4gh.passphrase")

	// Make sure the key path and passphrase is valid
	keyFile, err := os.Open(keyPath)
	if err != nil {
		return nil, err
	}

	key, err := keys.ReadPrivateKey(keyFile, []byte(passphrase))
	if err != nil {
		return nil, err
	}

	keyFile.Close()

	return &key, nil
}

// GetC4GHPublicKey reads the c4gh public key
func GetC4GHPublicKey() (*[32]byte, error) {
	keyPath := viper.GetString("c4gh.syncPubKeyPath")
	// Make sure the key path and passphrase is valid
	keyFile, err := os.Open(keyPath)
	if err != nil {
		return nil, err
	}

	key, err := keys.ReadPublicKey(keyFile)
	if err != nil {
		return nil, err
	}

	keyFile.Close()

	return &key, nil
}

func (c *Config) configServer() error {
	s := ServerConfig{}

	if !(viper.IsSet("server.jwtpubkeypath") || viper.IsSet("server.jwtpubkeyurl")) {
		return errors.New("either server.pubkeypath or server.jwtpubkeyurl should be present to start the service")
	}

	// Token authentication
	if viper.IsSet("server.jwtpubkeypath") {
		s.Jwtpubkeypath = viper.GetString("server.jwtpubkeypath")
	}

	if viper.IsSet("server.jwtpubkeyurl") {
		s.Jwtpubkeyurl = viper.GetString("server.jwtpubkeyurl")
	}

	if viper.IsSet("server.cert") {
		s.Cert = viper.GetString("server.cert")
	}
	if viper.IsSet("server.key") {
		s.Key = viper.GetString("server.key")
	}

	c.Server = s

	return nil
}

// TLSConfigBroker is a helper method to setup TLS for the message broker
func TLSConfigBroker(c *Config) (*tls.Config, error) {
	cfg := new(tls.Config)

	log.Debug("setting up TLS for broker connection")

	// Read system CAs
	systemCAs, err := x509.SystemCertPool()
	if err != nil {
		log.Errorf("failed to read system CAs: %v", err)

		return nil, err
	}

	cfg.RootCAs = systemCAs

	if c.Broker.CACert != "" {
		cacert, e := os.ReadFile(c.Broker.CACert) // #nosec this file comes from our configuration
		if e != nil {
			return nil, fmt.Errorf("failed to append %q to RootCAs: %v", cacert, e)
		}
		if ok := cfg.RootCAs.AppendCertsFromPEM(cacert); !ok {
			log.Debug("no certs appended, using system certs only")
		}
	}

	// This might be a bad thing to do globally, but we'll see.
	if c.Broker.ServerName != "" {
		cfg.ServerName = c.Broker.ServerName
	}

	if c.Broker.VerifyPeer {
		cert, e := os.ReadFile(c.Broker.ClientCert)
		if e != nil {
			return nil, fmt.Errorf("failed to read client cert %q, reason: %v", c.Broker.ClientKey, e)
		}
		key, e := os.ReadFile(c.Broker.ClientKey)
		if e != nil {
			return nil, fmt.Errorf("failed to read client key %q, reason: %v", c.Broker.ClientKey, e)
		}
		if certs, e := tls.X509KeyPair(cert, key); e == nil {
			cfg.Certificates = append(cfg.Certificates, certs)
		}
	}

	return cfg, nil
}

// TLSConfigProxy is a helper method to setup TLS for the S3 backend.
func TLSConfigProxy(c *Config) (*tls.Config, error) {
	cfg := new(tls.Config)

	log.Debug("setting up TLS for S3 connection")

	// Read system CAs
	systemCAs, err := x509.SystemCertPool()
	if err != nil {
		log.Errorf("failed to read system CAs: %v", err)

		return nil, err
	}

	cfg.RootCAs = systemCAs

	if c.Inbox.S3.CAcert != "" {
		cacert, e := os.ReadFile(c.Inbox.S3.CAcert) // #nosec this file comes from our configuration
		if e != nil {
			return nil, fmt.Errorf("failed to append %q to RootCAs: %v", cacert, e)
		}
		if ok := cfg.RootCAs.AppendCertsFromPEM(cacert); !ok {
			log.Debug("no certs appended, using system certs only")
		}
	}

	return cfg, nil
}
