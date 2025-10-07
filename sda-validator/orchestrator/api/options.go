package api

func ValidatorPaths(v []string) func(*validatorAPIImpl) {
	return func(impl *validatorAPIImpl) {
		impl.validatorPaths = v
	}
}

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
