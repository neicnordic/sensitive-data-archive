package job_worker

import (
	"github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/internal/broker"
	commandExecutor "github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/internal/command_executor"
)

type config struct {
	workerCount     int
	sourceQueue     string
	broker          broker.AMQPBrokerI
	commandExecutor commandExecutor.CommandExecutor
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

func CommandExecutor(v commandExecutor.CommandExecutor) func(*config) {
	return func(opts *config) {
		opts.commandExecutor = v
	}
}
