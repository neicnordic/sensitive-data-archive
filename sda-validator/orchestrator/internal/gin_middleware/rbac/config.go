package rbac

import (
	"github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/internal/config"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

var rbacPolicyFilePath string

func init() {
	config.RegisterFlags(
		&config.Flag{
			Name: "rbac.policy-file-path",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "/rbac/rbac.json", "Path to file containing rbac policy")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				rbacPolicyFilePath = viper.GetString(flagName)
			},
		},
	)
}
