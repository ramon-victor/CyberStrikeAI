//go:build !linux

package storage

// errPlatformUnsupported is converted to Available=false by the caller; not treated as fatal.
var errPlatformUnsupported = &unsupportedPlatformError{}

type unsupportedPlatformError struct{}

func (e *unsupportedPlatformError) Error() string {
	return "filesystem usage query is only supported on linux"
}

func filesystemUsage(path string) (Filesystem, error) {
	return Filesystem{Path: path}, errPlatformUnsupported
}
