package storageerrors

import "errors"

var ErrorFileNotFoundInLocation = errors.New("file not found in location")
var ErrorNoValidLocations = errors.New("no valid locations")
var ErrorInvalidLocation = errors.New("invalid location")
var ErrorNoEndpointConfiguredForLocation = errors.New("no endpoint configured for location")
var ErrorNoValidReader = errors.New("no valid reader configured")
