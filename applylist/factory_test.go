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
	whitelistPath     string
	fs                sysutil.FileSystemInterface
	expectedApplyList []string
	expectedBlacklist []string
	expectedErr       error
}

// TestPurgeComments verifies the comment processing and purging from the
// whitelist and blacklist specifications.
func TestPurgeComments(t *testing.T) {

	var testData = []struct {
		rawList        []string
		expectedReturn []string
	}{
		// No comment
		{
			[]string{"a/b.json", "b/c", "a/b/c.yaml", "a/b/c", "c.json"},
			[]string{"a/b.json", "b/c", "a/b/c.yaml", "a/b/c", "c.json"},
		},
		// First line is commented
		{
			[]string{"#a/b.json", "b/c", "a/b/c.yaml", "a/b/c", "c.json"},
			[]string{"b/c", "a/b/c.yaml", "a/b/c", "c.json"},
		},
		// Last line is commented
		{
			[]string{"a/b.json", "b/c", "a/b/c.yaml", "a/b/c", "# c.json"},
			[]string{"a/b.json", "b/c", "a/b/c.yaml", "a/b/c"},
		},
		// Empty line
		{
			[]string{"a/b.json", "", "a/b/c.yaml", "a/b/c", "c.json"},
			[]string{"a/b.json", "a/b/c.yaml", "a/b/c", "c.json"},
		},
		// Comment line only containing the comment character.
		{
			[]string{"a/b.json", "#", "a/b/c.yaml", "a/b/c", "c.json"},
			[]string{"a/b.json", "a/b/c.yaml", "a/b/c", "c.json"},
		},
		// Empty file
		{
			[]string{},
			[]string{},
		},
		// File with only comment lines.
		{
			[]string{"# some comment "},
			[]string{},
		},
	}

	assert := assert.New(t)
	mockCtrl := gomock.NewController(t)
	fs := sysutil.NewMockFileSystemInterface(mockCtrl)
	f := &Factory{"", "", "", fs}
	for _, td := range testData {

		rv := f.purgeCommentsFromList(td.rawList)
		assert.Equal(rv, td.expectedReturn)
	}
}

func TestFactoryCreate(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()
	fs := sysutil.NewMockFileSystemInterface(mockCtrl)

	// ReadLines error -> return nil lists and error, ListAllFiles not called
	gomock.InOrder(
		fs.EXPECT().ReadLines("/blacklist").Times(1).Return(nil, fmt.Errorf("error")),
	)
	tc := testCase{"/repo", "/blacklist", "/whitelist", fs, nil, nil, fmt.Errorf("error")}
	createAndAssert(t, tc)

	// ListAllFiles error -> return nil lists and error, ReadLines is called
	gomock.InOrder(
		fs.EXPECT().ReadLines("/blacklist").Times(1).Return([]string{}, nil),
		fs.EXPECT().ReadLines("/whitelist").Times(1).Return([]string{}, nil),
		fs.EXPECT().ListAllFiles("/repo").Times(1).Return(nil, fmt.Errorf("error")),
	)
	tc = testCase{"/repo", "/blacklist", "/whitelist", fs, nil, nil, fmt.Errorf("error")}
	createAndAssert(t, tc)

	// All lists and paths empty -> both lists empty, ReadLines not called
	gomock.InOrder(
		fs.EXPECT().ListAllFiles("").Times(1).Return([]string{}, nil),
	)
	tc = testCase{"", "", "", fs, []string{}, []string{}, nil}
	createAndAssert(t, tc)

	// Single .json file, empty blacklist -> file in applyList
	gomock.InOrder(
		fs.EXPECT().ReadLines("/blacklist").Times(1).Return([]string{}, nil),
		fs.EXPECT().ReadLines("/whitelist").Times(1).Return([]string{}, nil),
		fs.EXPECT().ListAllFiles("/repo").Times(1).Return([]string{"a.json"}, nil),
	)
	tc = testCase{"/repo", "/blacklist", "/whitelist", fs, []string{"a.json"}, []string{}, nil}
	createAndAssert(t, tc)

	// Single .yaml file, empty blacklist empty whitelist -> file in applyList
	gomock.InOrder(
		fs.EXPECT().ReadLines("/blacklist").Times(1).Return([]string{}, nil),
		fs.EXPECT().ReadLines("/whitelist").Times(1).Return([]string{}, nil),
		fs.EXPECT().ListAllFiles("/repo").Times(1).Return([]string{"a.yaml"}, nil),
	)
	tc = testCase{"/repo", "/blacklist", "/whitelist", fs, []string{"a.yaml"}, []string{}, nil}
	createAndAssert(t, tc)

	// Single non-.json & non-.yaml file, empty blacklist empty whitelist
	// -> file not in applyList
	gomock.InOrder(
		fs.EXPECT().ReadLines("/blacklist").Times(1).Return([]string{}, nil),
		fs.EXPECT().ReadLines("/whitelist").Times(1).Return([]string{}, nil),
		fs.EXPECT().ListAllFiles("/repo").Times(1).Return([]string{"a"}, nil),
	)
	tc = testCase{"/repo", "/blacklist", "/whitelist", fs, []string{}, []string{}, nil}
	createAndAssert(t, tc)

	// Multiple files (mixed extensions), empty blacklist, empty whitelist
	gomock.InOrder(
		fs.EXPECT().ReadLines("/blacklist").Times(1).Return([]string{}, nil),
		fs.EXPECT().ReadLines("/whitelist").Times(1).Return([]string{}, nil),
		fs.EXPECT().ListAllFiles("/repo").Times(1).Return([]string{"a.json", "b.jpg", "a/b.yaml", "a/b"}, nil),
	)
	tc = testCase{"/repo", "/blacklist", "/whitelist", fs, []string{"a.json", "a/b.yaml"}, []string{}, nil}
	createAndAssert(t, tc)

	// Multiple files (mixed extensions), blacklist, empty whitelist
	gomock.InOrder(
		fs.EXPECT().ReadLines("/blacklist").Times(1).Return([]string{"b.json", "b/c.json"}, nil),
		fs.EXPECT().ReadLines("/whitelist").Times(1).Return([]string{}, nil),
		fs.EXPECT().ListAllFiles("/repo").Times(1).Return([]string{"a.json", "b.json", "a/b/c.yaml", "a/b", "b/c.json"}, nil),
	)
	tc = testCase{"/repo", "/blacklist", "/whitelist", fs, []string{"a.json", "a/b/c.yaml"}, []string{"b.json", "b/c.json"}, nil}
	createAndAssert(t, tc)

	// File in blacklist but not in repo
	// (Ends up on returned blacklist anyway)
	gomock.InOrder(
		fs.EXPECT().ReadLines("/blacklist").Times(1).Return([]string{"a/b/c.yaml", "f.json"}, nil),
		fs.EXPECT().ReadLines("/whitelist").Times(1).Return([]string{}, nil),
		fs.EXPECT().ListAllFiles("/repo").Times(1).Return([]string{"a/b.json", "b/c", "a/b/c.yaml", "a/b/c", "c.json"}, nil),
	)
	tc = testCase{"/repo", "/blacklist", "/whitelist", fs, []string{"a/b.json", "c.json"}, []string{"a/b/c.yaml", "f.json"}, nil}
	createAndAssert(t, tc)

	// Empty blacklist, valid whitelist all whitelist is in the repo
	gomock.InOrder(
		fs.EXPECT().ReadLines("/blacklist").Times(1).Return([]string{}, nil),
		fs.EXPECT().ReadLines("/whitelist").Times(1).Return([]string{"a/b/c.yaml", "c.json"}, nil),
		fs.EXPECT().ListAllFiles("/repo").Times(1).Return([]string{"a/b.json", "b/c", "a/b/c.yaml", "a/b/c", "c.json"}, nil),
	)
	tc = testCase{"/repo", "/blacklist", "/whitelist", fs, []string{"a/b/c.yaml", "c.json"}, []string{}, nil}
	createAndAssert(t, tc)

	// Empty blacklist, valid whitelist some whitelist is not included in repo
	gomock.InOrder(
		fs.EXPECT().ReadLines("/blacklist").Times(1).Return([]string{}, nil),
		fs.EXPECT().ReadLines("/whitelist").Times(1).Return([]string{"a/b/c.yaml", "c.json", "someRandomFile.yaml"}, nil),
		fs.EXPECT().ListAllFiles("/repo").Times(1).Return([]string{"a/b.json", "b/c", "a/b/c.yaml", "a/b/c", "c.json"}, nil),
	)
	tc = testCase{"/repo", "/blacklist", "/whitelist", fs, []string{"a/b/c.yaml", "c.json"}, []string{}, nil}
	createAndAssert(t, tc)

	// Both whitelist and blacklist contain the same file
	gomock.InOrder(
		fs.EXPECT().ReadLines("/blacklist").Times(1).Return([]string{"a/b/c.yaml"}, nil),
		fs.EXPECT().ReadLines("/whitelist").Times(1).Return([]string{"a/b/c.yaml", "c.json"}, nil),
		fs.EXPECT().ListAllFiles("/repo").Times(1).Return([]string{"a/b.json", "b/c", "a/b/c.yaml", "a/b/c", "c.json"}, nil),
	)
	tc = testCase{"/repo", "/blacklist", "/whitelist", fs, []string{"c.json"}, []string{"a/b/c.yaml"}, nil}
	createAndAssert(t, tc)

	// Both whitelist and blacklist contain the same file and other comments.
	gomock.InOrder(
		fs.EXPECT().ReadLines("/blacklist").Times(1).Return([]string{"a/b/c.yaml", "#   c.json"}, nil),
		fs.EXPECT().ReadLines("/whitelist").Times(1).Return([]string{"a/b/c.yaml", "c.json", "#   a/b/c.yaml"}, nil),
		fs.EXPECT().ListAllFiles("/repo").Times(1).Return([]string{"a/b.json", "b/c", "a/b/c.yaml", "a/b/c", "c.json"}, nil),
	)
	tc = testCase{"/repo", "/blacklist", "/whitelist", fs, []string{"c.json"}, []string{"a/b/c.yaml"}, nil}
	createAndAssert(t, tc)

}

func createAndAssert(t *testing.T, tc testCase) {
	assert := assert.New(t)
	f := &Factory{tc.repoPath, tc.blacklistPath, tc.whitelistPath, tc.fs}
	applyList, blacklist, _, err := f.Create()
	assert.Equal(tc.expectedApplyList, applyList)
	assert.Equal(tc.expectedBlacklist, blacklist)
	assert.Equal(tc.expectedErr, err)
}
