package api

import "github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/internal/broker"

func SdaApiUrl(v string) func(*validatorAPIImpl) {
	return func(impl *validatorAPIImpl) {
		impl.sdaApiUrl = v
	}
}

func SdaApiToken(v string) func(*validatorAPIImpl) {
	return func(impl *validatorAPIImpl) {
		impl.sdaApiToken = v
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
