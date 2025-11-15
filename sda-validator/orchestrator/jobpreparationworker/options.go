package jobpreparationworker

import "github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/internal/broker"

type config struct {
	workerCount       int
	sourceQueue       string
	destinationQueue  string
	sdaAPIURL         string
	sdaAPIToken       string // TODO TBD #989
	broker            broker.AMQPBrokerI
	validationWorkDir string
}

func WorkerCount(v int) func(*config) {
	return func(opts *config) {
		opts.workerCount = v
	}
}

func SourceQueue(v string) func(*config) {
	return func(opts *config) {
		opts.sourceQueue = v
	}
}
func DestinationQueue(v string) func(*config) {
	return func(opts *config) {
		opts.destinationQueue = v
	}
}

func Broker(v broker.AMQPBrokerI) func(*config) {
	return func(opts *config) {
		opts.broker = v
	}
}

func ValidationWorkDirectory(v string) func(*config) {
	return func(opts *config) {
		opts.validationWorkDir = v
	}
}

func SdaAPIToken(v string) func(*config) {
	return func(opts *config) {
		opts.sdaAPIToken = v
	}
}

func SdaAPIURL(v string) func(*config) {
	return func(opts *config) {
		opts.sdaAPIURL = v
	}
}
