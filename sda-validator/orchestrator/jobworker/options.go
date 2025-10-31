package jobworker

import (
	"github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/internal/broker"
	"github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/internal/commandexecutor"
)

type config struct {
	workerCount     int
	sourceQueue     string
	broker          broker.AMQPBrokerI
	commandExecutor commandexecutor.CommandExecutor
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

func Broker(v broker.AMQPBrokerI) func(*config) {
	return func(opts *config) {
		opts.broker = v
	}
}

func CommandExecutor(v commandexecutor.CommandExecutor) func(*config) {
	return func(opts *config) {
		opts.commandExecutor = v
	}
}
