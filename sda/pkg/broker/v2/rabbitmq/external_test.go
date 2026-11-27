package rabbitmq_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/neicnordic/sensitive-data-archive/pkg/broker/v2/rabbitmq"
	"github.com/stretchr/testify/require"
)

func TestExternalConfigurationConnectsToConfiguredAddress(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	accepted := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			_ = conn.Close()
		}
		accepted <- err
	}()

	cfg := rabbitmq.DefaultConfig()
	cfg.Host = "127.0.0.1"
	cfg.Port = listener.Addr().(*net.TCPAddr).Port
	cfg.User = "guest"
	cfg.Password = "guest"
	cfg.VHost = "/"
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err = rabbitmq.NewRabbitMQBroker(ctx, cfg)
	require.Error(t, err) // The listener does not speak AMQP.
	select {
	case err := <-accepted:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatal("broker did not connect to the configured listener")
	}
}
