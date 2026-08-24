package config_test

import "os"

// statDir is shared by tests that check whether a path exists and is a
// directory.
func statDir(path string) (os.FileInfo, error) {
	return os.Stat(path)
}
