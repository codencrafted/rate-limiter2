package storage

import "errors"

// Common storage errors
var (
	// ErrStorageFailure is returned when a storage operation fails
	ErrStorageFailure = errors.New("storage operation failed")
	
	// ErrKeyNotFound is returned when a key is not found
	ErrKeyNotFound = errors.New("key not found")
	
	// ErrInvalidArgument is returned when an invalid argument is provided
	ErrInvalidArgument = errors.New("invalid argument")
) 