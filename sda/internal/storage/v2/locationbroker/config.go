package locationbroker

import (
	"time"

	"github.com/spf13/viper"
)

type config struct {
	cacheTTL time.Duration
}

func loadConfig() *config {
	cacheTTL := time.Second * 60
	if viper.IsSet("location_broker.cache_ttl") {
		cacheTTL = viper.GetDuration("location_broker.cache_ttl")
	}

	return &config{
		cacheTTL: cacheTTL,
	}
}

// CacheTTL allows to override the value loaded from location_broker.cache_ttl when initialising a new location broker
func CacheTTL(v time.Duration) func(*config) {
	return func(c *config) {
		c.cacheTTL = v
	}
}
