package config

import (
	"errors"
	"fmt"
	"path"
	"strings"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

type Flag struct {
	Name         string
	RegisterFunc func(cmd *pflag.FlagSet, flagName string)
	Required     bool
	AssignFunc   func(flagName string)
}

var registeredFlags []*Flag

func RegisterFlags(flags ...*Flag) {
	registeredFlags = append(registeredFlags, flags...)
}

var command = &cobra.Command{
	Run: func(_ *cobra.Command, _ []string) {
		// Empty func such that cobra will evaluate required flags, etc
	},
}

func init() {

	viper.SetConfigName("config")
	viper.AddConfigPath(".")

	viper.AutomaticEnv()
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	viper.SetConfigType("yaml")

	if viper.IsSet("configPath") {
		configPath := viper.GetString("configPath")
		splitPath := strings.Split(strings.TrimLeft(configPath, "/"), "/")
		viper.AddConfigPath(path.Join(splitPath...))
	}

	if viper.IsSet("configFile") {
		viper.SetConfigFile(viper.GetString("configFile"))
	}

	_ = viper.ReadInConfig()
}

func Load() error {

	for _, flag := range registeredFlags {
		flag.RegisterFunc(command.Flags(), flag.Name)
	}

	if err := viper.BindPFlags(command.Flags()); err != nil {
		panic(err)
	}

	if err := command.Execute(); err != nil {
		log.Fatalf("%v", err)
	}

	var missingFlags error
	for _, flag := range registeredFlags {
		if !flag.Required {
			continue
		}

		if !viper.IsSet(flag.Name) {
			missingFlags = errors.Join(missingFlags, fmt.Errorf("missing required flag: %s", flag.Name))
			continue
		}

	}
	if missingFlags != nil {
		return missingFlags
	}
	for _, flag := range registeredFlags {
		flag.AssignFunc(flag.Name)
	}
	return nil
}
