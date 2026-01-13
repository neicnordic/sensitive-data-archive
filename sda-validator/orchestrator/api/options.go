package api

import "github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/internal/broker"

func SdaAPIURL(v string) func(*validatorAPIImpl) {
	return func(impl *validatorAPIImpl) {
		impl.sdaAPIURL = v
	}
}

func SdaAPIToken(v string) func(*validatorAPIImpl) {
	return func(impl *validatorAPIImpl) {
		impl.sdaAPIToken = v
	}
}

func ValidationJobPreparationQueue(v string) func(*validatorAPIImpl) {
	return func(impl *validatorAPIImpl) {
		impl.validationJobPreparationQueue = v
	}
}

func Broker(v broker.AMQPBrokerI) func(*validatorAPIImpl) {
	return func(impl *validatorAPIImpl) {
		impl.broker = v
	}
}

func ValidationFileSizeLimit(v int64) func(*validatorAPIImpl) {
	return func(impl *validatorAPIImpl) {
		impl.validationFileSizeLimit = v
	}
}
