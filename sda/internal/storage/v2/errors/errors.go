package errors

import "errors"

var ErrorFileNotFoundInLocation = errors.New("file not found in location")
var ErrorNoValidLocations = errors.New("no valid locations")
var ErrorInvalidLocations = errors.New("invalid valid location")
var ErrorS3ReaderNotInitialized = errors.New("s3 reader has not been initialized")
var ErrorPosixReaderNotInitialized = errors.New("posix reader has not been initialized")
var ErrorS3WriterNotInitialized = errors.New("s3 writer has not been initialized")
var ErrorNoFreeBucket = errors.New("no free bucket")
var ErrorPosixWriterNotInitialized = errors.New("posix writer has not been initialized")
var ErrorNoEndpointConfiguredForLocation = errors.New("no endpoint configured for location")
