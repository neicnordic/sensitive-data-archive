package config

import (
	"errors"
	"fmt"
	"os"
	"path"
	"strings"
	"text/tabwriter"

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
		// Empty func such that cobra will evaluate flags, etc
	},
}

func init() {

	viper.SetConfigName("config")

	viper.AutomaticEnv()
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_", ".", "_"))
	viper.SetConfigType("yaml")

	command.Flags().String("config-path", ".", "Set the path viper will look for the config file at")
	command.Flags().String("config-file", "", "Set the direct path to the config file")

	command.SetHelpFunc(func(cmd *cobra.Command, _ []string) {

		fmt.Println("Flags:")
		writer := tabwriter.NewWriter(os.Stdout, 1, 1, 8, ' ', 0)
		_, _ = fmt.Fprintln(writer, "  Name:\tEnv variable:\tType:\tUsage:\tDefault Value:\t")
		cmd.Flags().VisitAll(func(flag *pflag.Flag) {
			if flag.Name == "help" {
				return
			}

			flagType, usage := pflag.UnquoteUsage(flag)
			envVar := strings.ToUpper(strings.Replace(strings.Replace(flag.Name, "-", "_", -1), ".", "_", -1))
			_, _ = fmt.Fprintln(writer, fmt.Sprintf("  --%s\t%s\t%s\t%s\t%v\t", flag.Name, envVar, flagType, usage, flag.DefValue))
		})

		_ = writer.Flush()

		os.Exit(0)
	})
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

	if viper.IsSet("config-path") {
		configPath := viper.GetString("config-path")
		splitPath := strings.Split(strings.TrimLeft(configPath, "/"), "/")
		viper.AddConfigPath(path.Join(splitPath...))
	}

	if viper.IsSet("config-file") {
		viper.SetConfigFile(viper.GetString("config-file"))
	}

	_ = viper.ReadInConfig()

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
