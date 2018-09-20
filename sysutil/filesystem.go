package sysutil

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"
)

// ListDirs walks the directory tree rooted at the path and adds all non-directory file paths to a []string.
func ListDirs(rootPath string) ([]string, error) {
	var dirs []string
	files, err := ioutil.ReadDir(rootPath)
	if err != nil {
		return dirs, fmt.Errorf("Could not read %s error=(%v)", rootPath, err)
	}

	for _, file := range files {
		if file.IsDir() {
			dirs = append(dirs, filepath.Join(rootPath, file.Name()))
		}
	}
	return dirs, nil
}

// WaitForDir returns when the specified directory is located in the filesystem, or if there is an error opening the directory once it is found.
func WaitForDir(path string, clock ClockInterface, interval time.Duration) error {
	for {
		f, err := os.Stat(path)
		if err != nil {
			if !os.IsNotExist(err) {
				return fmt.Errorf("Error opening the directory at %v: %v", path, err)
			}
		} else if !f.IsDir() {
			return fmt.Errorf("Error: %v is not a directory", path)
		} else {
			break
		}
		clock.Sleep(interval)
	}
	return nil
}
