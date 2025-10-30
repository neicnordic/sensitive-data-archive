package rbac

import (
	"github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/internal/config"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

type rbacConfig struct {
	rbacPolicyFilePath string
}

var conf = &rbacConfig{}

func init() {
	config.RegisterFlags(
		&config.Flag{
			Name: "rbacCasbin.policy-file-path",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "/rbacCasbin/rbacCasbin.json", "Path to file containing rbacCasbin policy")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				conf.rbacPolicyFilePath = viper.GetString(flagName)
			},
		},
	)
}

func RbacPolicyFilePath(v string) func(*rbacConfig) {
	return func(c *rbacConfig) {
		c.rbacPolicyFilePath = v
	}
}

func (c *rbacConfig) clone() *rbacConfig {
	return &rbacConfig{
		rbacPolicyFilePath: c.rbacPolicyFilePath,
	}
}
