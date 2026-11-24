package brokerconfig

import (
	"os"
	"testing"
	"time"

	"github.com/neicnordic/sensitive-data-archive/pkg/broker/v2/rabbitmq"
	config "github.com/neicnordic/sensitive-data-archive/pkg/config/v2"
	"github.com/stretchr/testify/require"
)

func TestConfigFromFlagsAndEnvironment(t *testing.T) {
	t.Setenv("BROKER_HOST", "environment-host")
	t.Setenv("BROKER_USER", "test-user")
	t.Setenv("BROKER_PASSWORD", "test-password")
	t.Setenv("BROKER_SSL", "true")
	t.Setenv("BROKER_CA_CERT", "/ca.pem")
	t.Setenv("BROKER_CLIENT_CERT", "/client.pem")
	t.Setenv("BROKER_CLIENT_KEY", "/client-key.pem")
	t.Setenv("BROKER_SHUTDOWN_GRACE", "0s")

	args := os.Args
	os.Args = []string{"broker-test", "--broker.host=flag-host", "--broker.prefetch_count=0"}
	t.Cleanup(func() { os.Args = args })

	require.NoError(t, config.Load())
	want := rabbitmq.DefaultConfig()
	want.Host = "flag-host"
	want.User = "test-user"
	want.Password = "test-password"
	want.SSL = true
	want.CACert = "/ca.pem"
	want.ClientCert = "/client.pem"
	want.ClientKey = "/client-key.pem"
	want.PrefetchCount = 0
	want.ShutdownGrace = time.Duration(0)
	require.Equal(t, want, Config())
}
