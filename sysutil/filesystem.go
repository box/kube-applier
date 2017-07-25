package sysutil

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strings"
	"time"
)

// FileSystemInterface allows for mocking out the functionality of FileSystem to avoid calls to the actual file system during testing.
type FileSystemInterface interface {
	ReadLines(filePath string) ([]string, error)
}

// FileSystem provides utility functions for interacting with the file system.
type FileSystem struct{}

// ReadLines opens the file located at the path and reads each line into a []string.
func (fs *FileSystem) ReadLines(filePath string) ([]string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("Error opening the file at %v: %v", filePath, err)
	}
	defer f.Close()

	var result []string
	s := bufio.NewScanner(f)
	for s.Scan() {
		result = append(result, strings.TrimSpace(s.Text()))
	}
	if err := s.Err(); err != nil {
		return nil, fmt.Errorf("Error reading the file at %v: %v", filePath, err)
	}
	return result, nil
}

// WaitForDir returns when the specified directory is located in the filesystem, or if there is an error opening the directory once it is found.
func WaitForDir(path string, clock ClockInterface, interval time.Duration) error {
	log.Printf("Waiting for directory at %v...", path)
	for {
		f, err := os.Stat(path)
		if err != nil {
			if !os.IsNotExist(err) {
				return fmt.Errorf("Error opening the directory at %v: %v", path, err)
			}
		} else if !f.IsDir() {
			return fmt.Errorf("Error: %v is not a directory", path)
		} else {
			log.Printf("Found directory at %v", path)
			break
		}
		clock.Sleep(interval)
	}
	return nil
}
