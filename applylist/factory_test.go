package applylist

import (
	"fmt"
	"github.com/box/kube-applier/sysutil"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"testing"
)

type testCase struct {
	repoPath          string
	blacklistPath     string
	fs                sysutil.FileSystemInterface
	expectedApplyList []string
	expectedBlacklist []string
	expectedErr       error
}

func TestFactoryCreate(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()
	fs := sysutil.NewMockFileSystemInterface(mockCtrl)

	// ReadLines error -> return nil lists and error, ListAllFiles not called
	gomock.InOrder(
		fs.EXPECT().ReadLines("/blacklist").Times(1).Return(nil, fmt.Errorf("error")),
	)
	tc := testCase{"/repo", "/blacklist", fs, nil, nil, fmt.Errorf("error")}
	createAndAssert(t, tc)

	// ListAllFiles error -> return nil lists and error, ReadLines is called
	gomock.InOrder(
		fs.EXPECT().ReadLines("/blacklist").Times(1).Return([]string{}, nil),
		fs.EXPECT().ListAllFiles("/repo").Times(1).Return(nil, fmt.Errorf("error")),
	)
	tc = testCase{"/repo", "/blacklist", fs, nil, nil, fmt.Errorf("error")}
	createAndAssert(t, tc)

	// All lists and paths empty -> both lists empty, ReadLines not called
	gomock.InOrder(
		fs.EXPECT().ListAllFiles("").Times(1).Return([]string{}, nil),
	)
	tc = testCase{"", "", fs, []string{}, []string{}, nil}
	createAndAssert(t, tc)

	// Single .json file, empty blacklist -> file in applyList
	gomock.InOrder(
		fs.EXPECT().ReadLines("/blacklist").Times(1).Return([]string{}, nil),
		fs.EXPECT().ListAllFiles("/repo").Times(1).Return([]string{"/repo/a.json"}, nil),
	)
	tc = testCase{"/repo", "/blacklist", fs, []string{"/repo/a.json"}, []string{}, nil}
	createAndAssert(t, tc)

	// Single .yaml file, empty blacklist -> file in applyList
	gomock.InOrder(
		fs.EXPECT().ReadLines("/blacklist").Times(1).Return([]string{}, nil),
		fs.EXPECT().ListAllFiles("/repo").Times(1).Return([]string{"/repo/a.yaml"}, nil),
	)
	tc = testCase{"/repo", "/blacklist", fs, []string{"/repo/a.yaml"}, []string{}, nil}
	createAndAssert(t, tc)

	// Single non-.json & non-.yaml file, empty blacklist -> file not in applyList
	gomock.InOrder(
		fs.EXPECT().ReadLines("/blacklist").Times(1).Return([]string{}, nil),
		fs.EXPECT().ListAllFiles("/repo").Times(1).Return([]string{"/repo/a"}, nil),
	)
	tc = testCase{"/repo", "/blacklist", fs, []string{}, []string{}, nil}
	createAndAssert(t, tc)

	// Multiple files (mixed extensions), empty blacklist
	gomock.InOrder(
		fs.EXPECT().ReadLines("/blacklist").Times(1).Return([]string{}, nil),
		fs.EXPECT().ListAllFiles("/repo").Times(1).Return([]string{"/repo/a.json", "/repo/b.jpg", "/repo/a/b.yaml", "/repo/a/b"}, nil),
	)
	tc = testCase{"/repo", "/blacklist", fs, []string{"/repo/a.json", "/repo/a/b.yaml"}, []string{}, nil}
	createAndAssert(t, tc)

	// Multiple files (mixed extensions), blacklist
	gomock.InOrder(
		fs.EXPECT().ReadLines("/blacklist").Times(1).Return([]string{"b.json", "b/c.json"}, nil),
		fs.EXPECT().ListAllFiles("/repo").Times(1).Return([]string{"/repo/a.json", "/repo/b.json", "/repo/a/b/c.yaml", "/repo/a/b", "/repo/b/c.json"}, nil),
	)
	tc = testCase{"/repo", "/blacklist", fs, []string{"/repo/a.json", "/repo/a/b/c.yaml"}, []string{"/repo/b.json", "/repo/b/c.json"}, nil}
	createAndAssert(t, tc)

	// File in blacklist but not in repo
	// (Ends up on returned blacklist anyway)
	gomock.InOrder(
		fs.EXPECT().ReadLines("/blacklist").Times(1).Return([]string{"a/b/c.yaml", "f.json"}, nil),
		fs.EXPECT().ListAllFiles("/repo").Times(1).Return([]string{"/repo/a/b.json", "/repo/b/c", "/repo/a/b/c.yaml", "/repo/a/b/c", "/repo/c.json"}, nil),
	)
	tc = testCase{"/repo", "/blacklist", fs, []string{"/repo/a/b.json", "/repo/c.json"}, []string{"/repo/a/b/c.yaml", "/repo/f.json"}, nil}
	createAndAssert(t, tc)
}

func createAndAssert(t *testing.T, tc testCase) {
	assert := assert.New(t)
	f := &Factory{tc.repoPath, tc.blacklistPath, tc.fs}
	applyList, blacklist, err := f.Create()
	assert.Equal(tc.expectedApplyList, applyList)
	assert.Equal(tc.expectedBlacklist, blacklist)
	assert.Equal(tc.expectedErr, err)
}
