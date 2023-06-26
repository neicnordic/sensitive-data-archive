// Package config manages configuration
package config

import (
	"fmt"
	"os"
	"path"
	"strings"
	"time"

	"github.com/neicnordic/crypt4gh/keys"
	log "github.com/sirupsen/logrus"

	"sda-pipeline/internal/broker"
	"sda-pipeline/internal/database"
	"sda-pipeline/internal/storage"

	"github.com/pkg/errors"
	"github.com/spf13/viper"
)

const POSIX = "posix"
const S3 = "s3"
const SFTP = "sftp"

var requiredConfVars []string

// Config is a parent object for all the different configuration parts
type Config struct {
	Archive      storage.Conf
	Broker       broker.MQConf
	Inbox        storage.Conf
	Backup       storage.Conf
	Database     database.DBConf
	API          APIConf
	Notify       SMTPConf
	Orchestrator OrchestratorConf
}

type APIConf struct {
	CACert     string
	ServerCert string
	ServerKey  string
	Host       string
	Port       int
	Session    SessionConfig
	DB         *database.SQLdb
	MQ         *broker.AMQPBroker
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

// NewConfig initializes and parses the config file and/or environment using
// the viper library.
func NewConfig(app string) (*Config, error) {
	viper.SetConfigName("config")
	viper.AddConfigPath(".")
	viper.AutomaticEnv()
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.SetConfigType("yaml")
	if viper.IsSet("configPath") {
		configPath := viper.GetString("configPath")
		splitPath := strings.Split(strings.TrimLeft(configPath, "/"), "/")
		viper.AddConfigPath(path.Join(splitPath...))
	}

	if viper.IsSet("configFile") {
		viper.SetConfigFile(viper.GetString("configFile"))
	}

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			log.Infoln("No config file found, using ENVs only")
		} else {
			return nil, err
		}
	}

	switch app {
	case "api":
		requiredConfVars = []string{
			"broker.host", "broker.port", "broker.user", "broker.password", "broker.routingkey", "db.host", "db.port", "db.user", "db.password", "db.database",
		}
	case "intercept":
		// Intercept does not require these extra settings
		requiredConfVars = []string{
			"broker.host", "broker.port", "broker.user", "broker.password", "broker.queue",
		}
	case "mapper":
		// Mapper does not require broker.routingkey thus we remove it
		requiredConfVars = []string{
			"broker.host", "broker.port", "broker.user", "broker.password", "broker.queue", "db.host", "db.port", "db.user", "db.password", "db.database",
		}
	case "notify":
		requiredConfVars = []string{
			"broker.host", "broker.port", "broker.user", "broker.password", "broker.queue", "smtp.host", "smtp.port", "smtp.password", "smtp.from",
		}
	case "orchestrate":
		// Orchestrate requires broker connection, a series of
		// queues, and the project FQDN.
		requiredConfVars = []string{
			"broker.host", "broker.port",
			"broker.user", "broker.password",
			"project.fqdn",
		}
	default:
		requiredConfVars = []string{
			"broker.host", "broker.port", "broker.user", "broker.password", "broker.queue", "broker.routingkey", "db.host", "db.port", "db.user", "db.password", "db.database",
		}
	}

	if viper.GetString("archive.type") == S3 {
		requiredConfVars = append(requiredConfVars, []string{"archive.url", "archive.accesskey", "archive.secretkey", "archive.bucket"}...)
	} else if viper.GetString("archive.type") == POSIX {
		requiredConfVars = append(requiredConfVars, []string{"archive.location"}...)
	}

	if viper.GetString("inbox.type") == S3 {
		requiredConfVars = append(requiredConfVars, []string{"inbox.url", "inbox.accesskey", "inbox.secretkey", "inbox.bucket"}...)
	} else if viper.GetString("inbox.type") == POSIX {
		requiredConfVars = append(requiredConfVars, []string{"inbox.location"}...)
	}

	switch viper.GetString("backup.type") {
	case S3:
		requiredConfVars = append(requiredConfVars, []string{"backup.url", "backup.accesskey", "backup.secretkey", "backup.bucket"}...)
	case POSIX:
		requiredConfVars = append(requiredConfVars, []string{"backup.location"}...)
	case SFTP:
		requiredConfVars = append(requiredConfVars, []string{"backup.sftp.host", "backup.sftp.port", "backup.sftp.userName", "backup.sftp.pemKeyPath", "backup.sftp.pemKeyPass"}...)
	}

	for _, s := range requiredConfVars {
		if !viper.IsSet(s) {
			return nil, fmt.Errorf("%s not set", s)
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
			log.Printf("Log level '%s' not supported, setting to 'trace'", stringLevel)
			intLevel = log.TraceLevel
		}
		log.SetLevel(intLevel)
		log.Printf("Setting log level to '%s'", stringLevel)
	}

	c := &Config{}
	err := c.configBroker()
	if err != nil {
		return nil, err
	}
	viper.SetDefault("schema.type", "federated")
	c.configSchemas()
	switch app {
	case "api":
		err = c.configDatabase()
		if err != nil {
			return nil, err
		}

		err = c.configAPI()
		if err != nil {
			return nil, err
		}

		return c, nil
	case "ingest":
		c.configInbox()
		c.configArchive()

		err = c.configDatabase()
		if err != nil {
			return nil, err
		}

		return c, nil
	case "intercept":
		return c, nil
	case "verify":
		c.configArchive()

		err = c.configDatabase()
		if err != nil {
			return nil, err
		}

		return c, nil
	case "finalize":
		err = c.configDatabase()
		if err != nil {
			return nil, err
		}

		return c, nil
	case "backup":
		c.configArchive()
		c.configBackup()

		err = c.configDatabase()
		if err != nil {
			return nil, err
		}

		return c, nil
	case "mapper":
		c.configInbox()
		err = c.configDatabase()
		if err != nil {
			return nil, err
		}

		return c, nil
	case "notify":
		c.configSMTP()

		return c, nil
	case "orchestrate":
		c.configOrchestrator()

		return c, nil
	}

	return nil, fmt.Errorf("application '%s' doesn't exist", app)
}

// configSchemas configures the schemas to load depending on
// the type IDs of connection Federated EGA or isolate (stand-alone)
func (c *Config) configSchemas() {
	if viper.GetString("schema.type") == "federated" {
		c.Broker.SchemasPath = "file://schemas/federated/"
	} else {
		c.Broker.SchemasPath = "file://schemas/isolated/"
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

	s3.Port = 443
	s3.Region = "us-east-1"
	s3.NonExistRetryTime = 2 * time.Minute

	if viper.IsSet(prefix + ".port") {
		s3.Port = viper.GetInt(prefix + ".port")
	}

	if viper.IsSet(prefix + ".region") {
		s3.Region = viper.GetString(prefix + ".region")
	}

	if viper.IsSet(prefix + ".chunksize") {
		s3.Chunksize = viper.GetInt(prefix+".chunksize") * 1024 * 1024
	}

	if viper.IsSet(prefix + ".cacert") {
		s3.Cacert = viper.GetString(prefix + ".cacert")
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
	broker.ServerName = viper.GetString("broker.serverName")

	if viper.IsSet("broker.routingkey") {
		broker.RoutingKey = viper.GetString("broker.routingkey")
	}

	if viper.IsSet("broker.exchange") {
		broker.Exchange = viper.GetString("broker.exchange")
	}

	if viper.IsSet("broker.durable") {
		broker.Durable = viper.GetBool("broker.durable")
	}
	if viper.IsSet("broker.routingerror") {
		broker.RoutingError = viper.GetString("broker.routingerror")
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

// configNotify provides configuration for the backup storage
func (c *Config) configSMTP() {
	c.Notify = SMTPConf{}
	c.Notify.Host = viper.GetString("smtp.host")
	c.Notify.Port = viper.GetInt("smtp.port")
	c.Notify.Password = viper.GetString("smtp.password")
	c.Notify.FromAddr = viper.GetString("smtp.from")
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
	keyPath := viper.GetString("c4gh.backupPubKey")

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

// CopyHeader reads the config and returns if the header will be copied
func CopyHeader() bool {
	if viper.IsSet("backup.copyHeader") {
		return viper.GetBool("backup.copyHeader")
	}

	return false
}
