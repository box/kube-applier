package sysutil

import (
	"fmt"
	"io/ioutil"
	"math/rand"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/go-test/deep"
)

// TestCopyFile tests that a copied file has the same mode, uid, gid and
// contents as the src file
func TestCopyFile(t *testing.T) {
	tmpDir, err := ioutil.TempDir("", "")
	defer os.RemoveAll(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	src := filepath.Join(tmpDir, "src")
	dst := filepath.Join(tmpDir, "dst")

	if err := writeFile(src); err != nil {
		t.Fatal(err)
	}

	if err := CopyFile(src, dst); err != nil {
		t.Fatal(err)
	}

	srcFileInfo, err := os.Lstat(src)
	if err != nil {
		t.Fatal(err)
	}
	dstFileInfo, err := os.Lstat(dst)
	if err != nil {
		t.Fatal(err)
	}
	srcFileSys := srcFileInfo.Sys().(*syscall.Stat_t)
	dstFileSys := dstFileInfo.Sys().(*syscall.Stat_t)
	if diff := deep.Equal(srcFileSys.Mode, dstFileSys.Mode); diff != nil {
		t.Error(diff)
	}
	if diff := deep.Equal(srcFileSys.Uid, dstFileSys.Uid); diff != nil {
		t.Error(diff)
	}
	if diff := deep.Equal(srcFileSys.Gid, dstFileSys.Gid); diff != nil {
		t.Error(diff)
	}

	srcBuf, err := ioutil.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	dstBuf, err := ioutil.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if diff := deep.Equal(srcBuf, dstBuf); diff != nil {
		t.Error(diff)
	}
}

// TestCopyDir tests that the copied files and dirs match the mode, uid, gid and
// contents of the corresponding src files
func TestCopyDir(t *testing.T) {
	src, err := ioutil.TempDir("", "src")
	defer os.RemoveAll(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := populateSrcDir(src); err != nil {
		t.Fatal(err)
	}

	dst, err := ioutil.TempDir("", "dst")
	defer os.RemoveAll(dst)
	if err != nil {
		t.Fatal(err)
	}

	if err := CopyDir(src, dst); err != nil {
		t.Fatal(err)
	}

	if err := filepath.Walk(src, func(srcPath string, f os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relativePath, err := filepath.Rel(src, srcPath)
		if err != nil {
			return err
		}
		if relativePath == "." {
			return nil
		}

		dstPath := filepath.Join(dst, relativePath)
		dstFileInfo, err := os.Lstat(dstPath)
		if err != nil {
			return err
		}
		srcFileSys := f.Sys().(*syscall.Stat_t)
		dstFileSys := dstFileInfo.Sys().(*syscall.Stat_t)
		if diff := deep.Equal(srcFileSys.Mode, dstFileSys.Mode); diff != nil {
			t.Error(diff)
		}
		if diff := deep.Equal(srcFileSys.Uid, dstFileSys.Uid); diff != nil {
			t.Error(diff)
		}
		if diff := deep.Equal(srcFileSys.Gid, dstFileSys.Gid); diff != nil {
			t.Error(diff)
		}

		if !f.IsDir() {
			srcBuf, err := ioutil.ReadFile(srcPath)
			if err != nil {
				return err
			}
			dstBuf, err := ioutil.ReadFile(dstPath)
			if err != nil {
				return err
			}
			if diff := deep.Equal(srcBuf, dstBuf); diff != nil {
				t.Error(diff)
			}
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// populateSrcDir writes a structure like this with different file modes and
// file contents:
// /tmp/dir-0
// /tmp/dir-0/file-0
// /tmp/dir-0/file-1
// /tmp/dir-0/subdir-0
// /tmp/dir-0/subdir-0/file-0
// /tmp/dir-0/subdir-0/file-1
// /tmp/dir-0/subdir-1
// /tmp/dir-0/subdir-1/file-0
// /tmp/dir-0/subdir-1/file-1
// /tmp/dir-1
// /tmp/dir-1/file-0
// /tmp/dir-1/file-1
// /tmp/dir-1/subdir-0
// /tmp/dir-1/subdir-0/file-0
// /tmp/dir-1/subdir-0/file-1
// /tmp/dir-1/subdir-1
// /tmp/dir-1/subdir-1/file-0
// /tmp/dir-1/subdir-1/file-1
func populateSrcDir(srcDir string) error {
	for i := 0; i < 2; i++ {
		dirName := filepath.Join(srcDir, fmt.Sprintf("dir-%d", i))
		if err := os.Mkdir(dirName, randomMode(0700)); err != nil {
			return err
		}
		for j := 0; j < 2; j++ {
			if err := writeFile(filepath.Join(dirName, fmt.Sprintf("file-%d", j))); err != nil {
				return err
			}
			subDirName := filepath.Join(srcDir, fmt.Sprintf("dir-%d/subdir-%d", i, j))
			if err := os.Mkdir(subDirName, randomMode(0700)); err != nil {
				return err
			}
			for k := 0; k < 2; k++ {
				if err := writeFile(filepath.Join(subDirName, fmt.Sprintf("file-%d", k))); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

// writeFile writes random data to the given file with a random filemode
func writeFile(name string) error {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	buf := make([]byte, 1024)
	if _, err := r.Read(buf); err != nil {
		return err
	}

	if err := ioutil.WriteFile(name, buf, randomMode(0700)); err != nil {
		return err
	}

	return nil
}

// randomMode generates a random file mode
func randomMode(baseMode int) os.FileMode {
	for i := 0; i < 7; i++ {
		baseMode = baseMode | (1&rand.Intn(2))<<uint(i)
	}
	return os.FileMode(baseMode)
}
