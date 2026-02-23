// Package config provides a flag-based configuration framework that allows
// applications to register their own configuration options.
//
// Usage:
//
//	// In your app's config package (e.g., cmd/download/config/config.go):
//	func init() {
//	    config.RegisterFlags(
//	        &config.Flag{
//	            Name: "api.port",
//	            RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
//	                flagSet.Int(flagName, 8080, "Port to host the API server at")
//	            },
//	            Required: false,
//	            AssignFunc: func(flagName string) {
//	                apiPort = viper.GetInt(flagName)
//	            },
//	        },
//	    )
//	}
//
//	// In your main.go:
//	if err := config.Load(); err != nil {
//	    log.Fatalf("failed to load config: %v", err)
//	}
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

// Flag represents a configuration flag that can be registered by applications.
type Flag struct {
	// Name is the flag name, e.g., "api.port" or "db.host"
	Name string
	// RegisterFunc registers the flag with the given FlagSet
	RegisterFunc func(flagSet *pflag.FlagSet, flagName string)
	// Required indicates if the flag must be set
	Required bool
	// AssignFunc assigns the flag value to the application's config variable
	AssignFunc func(flagName string)
}

var registeredFlags []*Flag

// RegisterFlags registers configuration flags. Call this from your app's config
// package init() function.
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
	command.Flags().String("log.level", "INFO", "Set the log level, supported levels: PANIC, FATAL, ERROR, WARN, INFO, DEBUG, TRACE")

	command.SetHelpFunc(func(cmd *cobra.Command, _ []string) {
		fmt.Println("Flags:")
		writer := tabwriter.NewWriter(os.Stdout, 1, 1, 8, ' ', 0)
		_, _ = fmt.Fprintln(writer, "  Name:\tEnv variable:\tType:\tUsage:\tDefault Value:\t")
		cmd.Flags().VisitAll(func(flag *pflag.Flag) {
			if flag.Name == "help" {
				return
			}

			flagType, usage := pflag.UnquoteUsage(flag)
			envVar := strings.ToUpper(strings.ReplaceAll(strings.ReplaceAll(flag.Name, "-", "_"), ".", "_"))
			_, _ = fmt.Fprintf(writer, "  --%s\t%s\t%s\t%s\t%v\t\n", flag.Name, envVar, flagType, usage, flag.DefValue)
		})

		_ = writer.Flush()

		os.Exit(0)
	})
}

// Load loads configuration from flags, environment variables, and config files.
// It validates that all required flags are set and assigns values to application
// config variables via their AssignFunc.
func Load() error {
	for _, flag := range registeredFlags {
		flag.RegisterFunc(command.Flags(), flag.Name)
	}

	if err := viper.BindPFlags(command.Flags()); err != nil {
		panic(err)
	}

	if err := command.Execute(); err != nil {
		return err
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

	stringLevel := viper.GetString("log.level")
	logLevel, err := log.ParseLevel(stringLevel)
	if err != nil {
		log.Debugf("Log level '%s' not supported, setting to 'trace'", stringLevel)
		logLevel = log.TraceLevel
	}
	log.SetLevel(logLevel)
	log.Infof("Setting log level to '%s'", stringLevel)

	return nil
}
