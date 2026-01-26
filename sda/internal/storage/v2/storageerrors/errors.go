package storageerrors

import "errors"

var ErrorFileNotFoundInLocation = errors.New("file not found in location")
var ErrorNoValidLocations = errors.New("no valid locations")
var ErrorInvalidLocation = errors.New("invalid location")
var ErrorNoFreeBucket = errors.New("no free bucket")
var ErrorNoEndpointConfiguredForLocation = errors.New("no endpoint configured for location")
var ErrorNoValidWriter = errors.New("no valid writer configured")
var ErrorNoValidReader = errors.New("no valid reader configured")
var ErrorMultipleWritersNotSupported = errors.New("s3 writer and posix writer cannot be used at the same time")
