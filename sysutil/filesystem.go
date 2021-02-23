package sysutil

import (
	"fmt"
	"os"
	"time"

	"github.com/utilitywarehouse/kube-applier/log"
)

// ListDirs returns a list of all the subdirectories of the rootPath.
func ListDirs(rootPath string) ([]string, error) {
	var dirs []string
	files, err := os.ReadDir(rootPath)
	if err != nil {
		return dirs, fmt.Errorf("Could not read %s error=(%v)", rootPath, err)
	}

	for _, file := range files {
		if file.IsDir() {
			dirs = append(dirs, file.Name())
		}
	}
	return dirs, nil
}

// WaitForDir returns when the specified directory is located in the filesystem, or if there is an error opening the directory once it is found.
func WaitForDir(path string, interval, timeout time.Duration) error {
	log.Logger("filesystem").Info("Waiting for the source directory", "path", path, "timeout", timeout.String())

	to := time.After(timeout)

	tick := time.Tick(interval)

	for {
		select {

		case <-to:
			return fmt.Errorf(
				"timeout waiting for dir: %v",
				path,
			)

		case <-tick:
			f, err := os.Stat(path)
			if err != nil {
				if !os.IsNotExist(err) {
					return fmt.Errorf(
						"Error opening the directory at %v: %v",
						path,
						err,
					)
				}
				log.Logger("filesystem").Debug("Failed to get dir info", "path", path, "error", err)
			} else if !f.IsDir() {
				return fmt.Errorf(
					"%v is not a directory",
					path,
				)
			} else {
				log.Logger("filesystem").Info("Found the source directory", "path", path)
				return nil
			}
		}
	}
}
