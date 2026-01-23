package reader

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/neicnordic/sensitive-data-archive/internal/storage/v2/storageerrors"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/suite"
)

type ReaderTestSuite struct {
	suite.Suite
	reader     *Reader
	dir1, dir2 string
}

func TestReaderTestSuite(t *testing.T) {
	suite.Run(t, new(ReaderTestSuite))
}

func (ts *ReaderTestSuite) SetupSuite() {
	ts.dir1 = ts.T().TempDir()
	ts.dir2 = ts.T().TempDir()

	if err := os.WriteFile(filepath.Join(ts.dir1, "config.yaml"), []byte(fmt.Sprintf(`
storage:
  test:
    posix:
    - path: %s
    - path: %s
`, ts.dir1, ts.dir2)), 0600); err != nil {
		ts.FailNow(err.Error())
	}

	// create some files
	for i := 0; i < 10; i++ {
		if err := os.WriteFile(filepath.Join(ts.dir1, fmt.Sprintf("dir1_file%d.txt", i)), []byte(fmt.Sprintf("file %d content in dir1", i)), 0600); err != nil {
			ts.FailNow(err.Error())
		}
		if err := os.WriteFile(filepath.Join(ts.dir2, fmt.Sprintf("dir2_file%d.txt", i)), []byte(fmt.Sprintf("file %d content in dir2", i)), 0600); err != nil {
			ts.FailNow(err.Error())
		}
	}

	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.SetConfigType("yaml")
	viper.SetConfigFile(filepath.Join(ts.dir1, "config.yaml"))

	if err := viper.ReadInConfig(); err != nil {
		ts.FailNow(err.Error())
	}

	var err error
	ts.reader, err = NewReader("test")
	if err != nil {
		ts.FailNow(err.Error())
	}
}

func (ts *ReaderTestSuite) TestNewFileReader() {
	for _, test := range []struct {
		testName            string
		locationToReadFrom  string
		filePathToRead      string
		expectedError       error
		expectedFileContent string
	}{
		{
			testName:            "FromDir1",
			expectedError:       nil,
			expectedFileContent: "file 2 content in dir1",
			locationToReadFrom:  ts.dir1,
			filePathToRead:      "dir1_file2.txt",
		},
		{
			testName:            "FromDir2",
			expectedError:       nil,
			expectedFileContent: "file 9 content in dir2",
			locationToReadFrom:  ts.dir2,
			filePathToRead:      "dir2_file9.txt",
		},
		{
			testName:            "FromDir1_FileNotExists",
			expectedError:       storageerrors.ErrorFileNotFoundInLocation,
			expectedFileContent: "",
			locationToReadFrom:  ts.dir1,
			filePathToRead:      "not_exists.txt",
		},
	} {
		ts.T().Run(test.testName, func(t *testing.T) {
			fileReader, err := ts.reader.NewFileReader(context.Background(), test.locationToReadFrom, test.filePathToRead)
			ts.Equal(test.expectedError, err)

			if fileReader != nil {
				content, err := io.ReadAll(fileReader)
				ts.NoError(err)

				ts.Equal(test.expectedFileContent, string(content))
				_ = fileReader.Close()
			}
		})
	}
}

func (ts *ReaderTestSuite) TestNewFileSeekReader_ReadFromDir() {
	fileSeekReader, err := ts.reader.NewFileReadSeeker(context.Background(), ts.dir2, "dir2_file5.txt")
	if err != nil {
		ts.FailNow(err.Error())
	}

	start := int64(5)
	lenToRead := int64(9)

	_, err = fileSeekReader.Seek(start, 0)
	ts.NoError(err)

	content := make([]byte, lenToRead)

	read, err := fileSeekReader.Read(content)
	ts.NoError(err)
	ts.Equal(lenToRead, int64(read))

	ts.Equal("5 content", string(content))
	_ = fileSeekReader.Close()
}

func (ts *ReaderTestSuite) TestGetFileSize() {
	fileSize, err := ts.reader.GetFileSize(context.Background(), ts.dir2, "dir2_file6.txt")
	if err != nil {
		ts.FailNow(err.Error())
	}

	ts.Equal(int64(len("file 6 content in dir2")), fileSize)
}

func (ts *ReaderTestSuite) TestFindFile() {
	for _, test := range []struct {
		testName         string
		filePathToFind   string
		expectedError    error
		expectedLocation string
	}{
		{
			testName:         "FoundInDir1",
			expectedError:    nil,
			expectedLocation: ts.dir1,
			filePathToFind:   "dir1_file3.txt",
		}, {
			testName:         "FoundInDir2",
			expectedError:    nil,
			expectedLocation: ts.dir2,
			filePathToFind:   "dir2_file9.txt",
		},
		{
			testName:         "NotFound",
			expectedError:    storageerrors.ErrorFileNotFoundInLocation,
			expectedLocation: "",
			filePathToFind:   "not_exists.txt",
		},
	} {
		ts.T().Run(test.testName, func(t *testing.T) {
			loc, err := ts.reader.FindFile(context.TODO(), test.filePathToFind)
			ts.Equal(test.expectedError, err)

			ts.Equal(test.expectedLocation, loc)
		})
	}
}
