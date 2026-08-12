package observability

import (
	"github.com/neicnordic/sensitive-data-archive/internal/config/v2"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

var (
	enabled bool
)

func init() {
	config.RegisterFlags(
		&config.Flag{
			Name: "observability.enabled",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.Bool(flagName, false, "If observability(metrics, tracing) is to be enabled, if enabled, Prometheus endpoint will be hosted at port 9090, and see [OpenTelemetry Environment Variable Specification](https://opentelemetry.io/docs/specs/otel/configuration/sdk-environment-variables/) for additional environment variable documentation")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				enabled = viper.GetBool(flagName)
			},
		},
	)
}
