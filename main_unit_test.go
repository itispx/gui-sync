package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3iface"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// Mock S3 client
type mockS3Client struct {
	s3iface.S3API
	mock.Mock
}

func (m *mockS3Client) HeadObject(input *s3.HeadObjectInput) (*s3.HeadObjectOutput, error) {
	args := m.Called(input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*s3.HeadObjectOutput), args.Error(1)
}

func (m *mockS3Client) PutObject(input *s3.PutObjectInput) (*s3.PutObjectOutput, error) {
	args := m.Called(input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*s3.PutObjectOutput), args.Error(1)
}

func (m *mockS3Client) DeleteObject(input *s3.DeleteObjectInput) (*s3.DeleteObjectOutput, error) {
	args := m.Called(input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*s3.DeleteObjectOutput), args.Error(1)
}

func (m *mockS3Client) ListObjectsV2Pages(input *s3.ListObjectsV2Input, fn func(*s3.ListObjectsV2Output, bool) bool) error {
	args := m.Called(input, mock.Anything)
	if output := args.Get(0); output != nil {
		fn(output.(*s3.ListObjectsV2Output), true)
	}
	return args.Error(1)
}

// Test helpers
func createTempFile(t *testing.T, dir, name, content string) string {
	path := filepath.Join(dir, name)
	err := os.MkdirAll(filepath.Dir(path), 0755)
	require.NoError(t, err)
	err = os.WriteFile(path, []byte(content), 0644)
	require.NoError(t, err)
	return path
}

// Test Suite: MD5 Calculation
func TestCalculateMD5(t *testing.T) {
	t.Run("calculate MD5 for small file", func(t *testing.T) {
		tempDir := t.TempDir()
		content := "test content for md5"
		filePath := createTempFile(t, tempDir, "test.txt", content)

		hash, err := calculateMD5(filePath)
		assert.NoError(t, err)
		assert.NotEmpty(t, hash)
		assert.Len(t, hash, 32) // MD5 is 32 hex chars
	})

	t.Run("calculate MD5 for empty file", func(t *testing.T) {
		tempDir := t.TempDir()
		filePath := createTempFile(t, tempDir, "empty.txt", "")

		hash, err := calculateMD5(filePath)
		assert.NoError(t, err)
		assert.NotEmpty(t, hash)
	})

	t.Run("error on non-existent file", func(t *testing.T) {
		_, err := calculateMD5("/non/existent/file.txt")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to open file")
	})

	t.Run("calculate MD5 for large content", func(t *testing.T) {
		tempDir := t.TempDir()
		content := strings.Repeat("large content test ", 10000)
		filePath := createTempFile(t, tempDir, "large.txt", content)

		hash, err := calculateMD5(filePath)
		assert.NoError(t, err)
		assert.Len(t, hash, 32) // MD5 hash is 32 hex characters
	})

	t.Run("consistent MD5 for same content", func(t *testing.T) {
		tempDir := t.TempDir()
		content := "consistent content"
		filePath1 := createTempFile(t, tempDir, "file1.txt", content)
		filePath2 := createTempFile(t, tempDir, "file2.txt", content)

		hash1, err1 := calculateMD5(filePath1)
		hash2, err2 := calculateMD5(filePath2)

		assert.NoError(t, err1)
		assert.NoError(t, err2)
		assert.Equal(t, hash1, hash2)
	})
}

// Test Suite: .syncignore Loading
func TestLoadSyncIgnoreFile(t *testing.T) {
	// Save original state
	originalRootDir := rootDir
	originalPatterns := ignorePatterns
	defer func() {
		rootDir = originalRootDir
		ignorePatterns = originalPatterns
	}()

	t.Run("load valid syncignore file", func(t *testing.T) {
		tempDir := t.TempDir()
		rootDir = tempDir
		ignorePatterns = nil

		syncignoreContent := `# Comment line
*.log
temp/
.git/

node_modules/`
		createTempFile(t, tempDir, ".syncignore", syncignoreContent)

		err := loadSyncIgnoreFile()
		assert.NoError(t, err)
		assert.Len(t, ignorePatterns, 4)
		assert.Contains(t, ignorePatterns, "*.log")
		assert.Contains(t, ignorePatterns, "temp/")
		assert.Contains(t, ignorePatterns, ".git/")
		assert.Contains(t, ignorePatterns, "node_modules/")
	})

	t.Run("handle missing syncignore file", func(t *testing.T) {
		tempDir := t.TempDir()
		rootDir = tempDir
		ignorePatterns = nil

		err := loadSyncIgnoreFile()
		assert.NoError(t, err)
		assert.Empty(t, ignorePatterns)
	})

	t.Run("ignore empty lines and comments", func(t *testing.T) {
		tempDir := t.TempDir()
		rootDir = tempDir
		ignorePatterns = nil

		syncignoreContent := `# This is a comment

*.tmp
  
# Another comment
build/`
		createTempFile(t, tempDir, ".syncignore", syncignoreContent)

		err := loadSyncIgnoreFile()
		assert.NoError(t, err)
		assert.Len(t, ignorePatterns, 2)
		assert.Contains(t, ignorePatterns, "*.tmp")
		assert.Contains(t, ignorePatterns, "build/")
	})

	t.Run("trim whitespace from patterns", func(t *testing.T) {
		tempDir := t.TempDir()
		rootDir = tempDir
		ignorePatterns = nil

		syncignoreContent := `  *.log  
	temp/	
   .git/   `
		createTempFile(t, tempDir, ".syncignore", syncignoreContent)

		err := loadSyncIgnoreFile()
		assert.NoError(t, err)
		assert.Len(t, ignorePatterns, 3)
		assert.Contains(t, ignorePatterns, "*.log")
		assert.Contains(t, ignorePatterns, "temp/")
		assert.Contains(t, ignorePatterns, ".git/")
	})
}

// Test Suite: shouldIgnore
func TestShouldIgnore(t *testing.T) {
	// Save original state
	originalPatterns := ignorePatterns
	defer func() {
		ignorePatterns = originalPatterns
	}()

	ignorePatterns = []string{"*.log", "temp/", ".git/", "node_modules/"}

	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{"ignore log pattern", "*.log", true},
		{"ignore temp directory", "temp/", true},
		{"ignore git directory", ".git/", true},
		{"ignore node_modules", "node_modules/", true},
		{"don't ignore normal file", "src/main.go", false},
		{"don't ignore txt file", "readme.txt", false},
		{"don't ignore similar pattern", "file.log.bak", false},
		{"exact match only", "temps/", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := shouldIgnore(tt.path)
			assert.Equal(t, tt.expected, result)
		})
	}

	t.Run("empty ignore patterns", func(t *testing.T) {
		ignorePatterns = []string{}
		assert.False(t, shouldIgnore("anything.txt"))
	})

	t.Run("case sensitive matching", func(t *testing.T) {
		ignorePatterns = []string{"Test.txt"}
		assert.True(t, shouldIgnore("Test.txt"))
		assert.False(t, shouldIgnore("test.txt"))
	})
}

// Test Suite: fileChangedOnS3
func TestFileChangedOnS3(t *testing.T) {
	// Save original state
	originalBucket := bucketName
	defer func() {
		bucketName = originalBucket
	}()

	bucketName = "test-bucket"

	t.Run("file not found on S3", func(t *testing.T) {
		mockClient := new(mockS3Client)
		tempDir := t.TempDir()
		filePath := createTempFile(t, tempDir, "new.txt", "new content")

		// Create a proper AWS 404 error
		awsErr := awserr.NewRequestFailure(
			awserr.New("NotFound", "Not Found", nil),
			404,
			"request-id",
		)

		mockClient.On("HeadObject", mock.Anything).Return(
			nil,
			awsErr,
		).Once()

		changed, err := fileChangedOnS3(mockClient, "new.txt", filePath)
		assert.NoError(t, err)
		assert.True(t, changed)
		mockClient.AssertExpectations(t)
	})

	t.Run("file size differs", func(t *testing.T) {
		mockClient := new(mockS3Client)
		tempDir := t.TempDir()
		content := "test content"
		filePath := createTempFile(t, tempDir, "test.txt", content)

		now := time.Now()
		mockClient.On("HeadObject", mock.Anything).Return(
			&s3.HeadObjectOutput{
				ContentLength: aws.Int64(100), // Different size
				LastModified:  &now,
				ETag:          aws.String("\"abc123\""),
			},
			nil,
		).Once()

		changed, err := fileChangedOnS3(mockClient, "test.txt", filePath)
		assert.NoError(t, err)
		assert.True(t, changed)
		mockClient.AssertExpectations(t)
	})

	t.Run("file unchanged - same size and older local modification", func(t *testing.T) {
		mockClient := new(mockS3Client)
		tempDir := t.TempDir()
		content := "test content"
		filePath := createTempFile(t, tempDir, "test.txt", content)

		fileInfo, _ := os.Stat(filePath)
		futureTime := fileInfo.ModTime().Add(time.Hour)

		mockClient.On("HeadObject", mock.Anything).Return(
			&s3.HeadObjectOutput{
				ContentLength: aws.Int64(fileInfo.Size()),
				LastModified:  &futureTime,
				ETag:          aws.String("\"abc123\""),
			},
			nil,
		).Once()

		changed, err := fileChangedOnS3(mockClient, "test.txt", filePath)
		assert.NoError(t, err)
		assert.False(t, changed)
		mockClient.AssertExpectations(t)
	})

	t.Run("large file - skip MD5 calculation", func(t *testing.T) {
		mockClient := new(mockS3Client)
		tempDir := t.TempDir()
		// Create a file larger than multipartThreshold
		largeContent := strings.Repeat("x", int(multipartThreshold+1))
		filePath := createTempFile(t, tempDir, "large.txt", largeContent)

		fileInfo, _ := os.Stat(filePath)
		pastTime := fileInfo.ModTime().Add(-time.Hour)

		mockClient.On("HeadObject", mock.Anything).Return(
			&s3.HeadObjectOutput{
				ContentLength: aws.Int64(fileInfo.Size()),
				LastModified:  &pastTime,
				ETag:          aws.String("\"abc123\""),
			},
			nil,
		).Once()

		changed, err := fileChangedOnS3(mockClient, "large.txt", filePath)
		assert.NoError(t, err)
		assert.True(t, changed) // Local file is newer
		mockClient.AssertExpectations(t)
	})

	t.Run("multipart upload ETag - skip MD5 comparison", func(t *testing.T) {
		mockClient := new(mockS3Client)
		tempDir := t.TempDir()
		content := "small content"
		filePath := createTempFile(t, tempDir, "test.txt", content)

		fileInfo, _ := os.Stat(filePath)
		pastTime := fileInfo.ModTime().Add(-time.Hour)

		mockClient.On("HeadObject", mock.Anything).Return(
			&s3.HeadObjectOutput{
				ContentLength: aws.Int64(fileInfo.Size()),
				LastModified:  &pastTime,
				ETag:          aws.String("\"abc123-5\""), // Multipart ETag
			},
			nil,
		).Once()

		changed, err := fileChangedOnS3(mockClient, "test.txt", filePath)
		assert.NoError(t, err)
		assert.True(t, changed)
		mockClient.AssertExpectations(t)
	})

	t.Run("S3 error other than 404", func(t *testing.T) {
		mockClient := new(mockS3Client)
		tempDir := t.TempDir()
		filePath := createTempFile(t, tempDir, "test.txt", "content")

		// Create a non-404 AWS error
		awsErr := awserr.NewRequestFailure(
			awserr.New("InternalError", "Internal Server Error", nil),
			500,
			"request-id",
		)

		mockClient.On("HeadObject", mock.Anything).Return(
			nil,
			awsErr,
		).Once()

		_, err := fileChangedOnS3(mockClient, "test.txt", filePath)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "error checking S3 object")
		mockClient.AssertExpectations(t)
	})
}

// Test Suite: deleteRemovedFilesFromS3
func TestDeleteRemovedFilesFromS3(t *testing.T) {
	// Save original state
	originalBucket := bucketName
	defer func() {
		bucketName = originalBucket
	}()

	bucketName = "test-bucket"

	t.Run("delete files not in local directory", func(t *testing.T) {
		mockClient := new(mockS3Client)
		tempDir := t.TempDir()
		createTempFile(t, tempDir, "keep.txt", "keep me")

		s3Objects := []*s3.Object{
			{Key: aws.String("keep.txt")},
			{Key: aws.String("delete.txt")},
			{Key: aws.String("old.txt")},
		}

		mockClient.On("ListObjectsV2Pages", mock.Anything, mock.Anything).Return(
			&s3.ListObjectsV2Output{Contents: s3Objects},
			nil,
		).Once()

		mockClient.On("DeleteObject", &s3.DeleteObjectInput{
			Bucket: aws.String("test-bucket"),
			Key:    aws.String("delete.txt"),
		}).Return(&s3.DeleteObjectOutput{}, nil).Once()

		mockClient.On("DeleteObject", &s3.DeleteObjectInput{
			Bucket: aws.String("test-bucket"),
			Key:    aws.String("old.txt"),
		}).Return(&s3.DeleteObjectOutput{}, nil).Once()

		err := deleteRemovedFilesFromS3(mockClient, tempDir)
		assert.NoError(t, err)
		mockClient.AssertExpectations(t)
	})

	t.Run("no deletions when all files exist locally", func(t *testing.T) {
		mockClient := new(mockS3Client)
		tempDir := t.TempDir()
		createTempFile(t, tempDir, "file1.txt", "content1")
		createTempFile(t, tempDir, "file2.txt", "content2")

		s3Objects := []*s3.Object{
			{Key: aws.String("file1.txt")},
			{Key: aws.String("file2.txt")},
		}

		mockClient.On("ListObjectsV2Pages", mock.Anything, mock.Anything).Return(
			&s3.ListObjectsV2Output{Contents: s3Objects},
			nil,
		).Once()

		err := deleteRemovedFilesFromS3(mockClient, tempDir)
		assert.NoError(t, err)
		mockClient.AssertExpectations(t)
	})

	t.Run("handle empty S3 bucket", func(t *testing.T) {
		mockClient := new(mockS3Client)
		tempDir := t.TempDir()
		createTempFile(t, tempDir, "file.txt", "content")

		mockClient.On("ListObjectsV2Pages", mock.Anything, mock.Anything).Return(
			&s3.ListObjectsV2Output{Contents: []*s3.Object{}},
			nil,
		).Once()

		err := deleteRemovedFilesFromS3(mockClient, tempDir)
		assert.NoError(t, err)
		mockClient.AssertExpectations(t)
	})

	t.Run("handle ListObjects error", func(t *testing.T) {
		mockClient := new(mockS3Client)
		tempDir := t.TempDir()

		mockClient.On("ListObjectsV2Pages", mock.Anything, mock.Anything).Return(
			nil,
			fmt.Errorf("access denied"),
		).Once()

		err := deleteRemovedFilesFromS3(mockClient, tempDir)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to delete files from S3")
		mockClient.AssertExpectations(t)
	})

	t.Run("handle nested directories", func(t *testing.T) {
		mockClient := new(mockS3Client)
		tempDir := t.TempDir()
		createTempFile(t, tempDir, "dir1/file1.txt", "content1")
		createTempFile(t, tempDir, "dir2/subdir/file2.txt", "content2")

		s3Objects := []*s3.Object{
			{Key: aws.String("dir1/file1.txt")},
			{Key: aws.String("dir2/subdir/file2.txt")},
			{Key: aws.String("dir3/old.txt")},
		}

		mockClient.On("ListObjectsV2Pages", mock.Anything, mock.Anything).Return(
			&s3.ListObjectsV2Output{Contents: s3Objects},
			nil,
		).Once()

		mockClient.On("DeleteObject", &s3.DeleteObjectInput{
			Bucket: aws.String("test-bucket"),
			Key:    aws.String("dir3/old.txt"),
		}).Return(&s3.DeleteObjectOutput{}, nil).Once()

		err := deleteRemovedFilesFromS3(mockClient, tempDir)
		assert.NoError(t, err)
		mockClient.AssertExpectations(t)
	})
}

// Test Suite: uploadFileS3
func TestUploadFileS3(t *testing.T) {
	// Save original state
	originalBucket := bucketName
	defer func() {
		bucketName = originalBucket
	}()

	bucketName = "test-bucket"

	t.Run("upload small file", func(t *testing.T) {
		mockClient := new(mockS3Client)
		tempDir := t.TempDir()
		content := "small file content"
		filePath := createTempFile(t, tempDir, "small.txt", content)

		mockClient.On("PutObject", mock.MatchedBy(func(input *s3.PutObjectInput) bool {
			return *input.Bucket == "test-bucket" && *input.Key == "small.txt"
		})).Return(&s3.PutObjectOutput{}, nil).Once()

		size, err := uploadFileS3(mockClient, nil, "small.txt", filePath, int64(len(content)))
		assert.NoError(t, err)
		assert.Equal(t, int64(len(content)), size)
		mockClient.AssertExpectations(t)
	})

	t.Run("error on non-existent file", func(t *testing.T) {
		mockClient := new(mockS3Client)
		_, err := uploadFileS3(mockClient, nil, "test.txt", "/non/existent.txt", 100)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to open file")
	})

	t.Run("upload error handling", func(t *testing.T) {
		mockClient := new(mockS3Client)
		tempDir := t.TempDir()
		content := "test content"
		filePath := createTempFile(t, tempDir, "test.txt", content)

		mockClient.On("PutObject", mock.Anything).Return(
			nil,
			fmt.Errorf("upload failed"),
		).Once()

		_, err := uploadFileS3(mockClient, nil, "test.txt", filePath, int64(len(content)))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to upload file to S3")
		mockClient.AssertExpectations(t)
	})
}

// Test Suite: Integration Tests
func TestIntegration(t *testing.T) {
	// Save original state
	originalRootDir := rootDir
	originalPatterns := ignorePatterns
	defer func() {
		rootDir = originalRootDir
		ignorePatterns = originalPatterns
	}()

	t.Run("full sync workflow", func(t *testing.T) {
		tempDir := t.TempDir()
		rootDir = tempDir

		// Create test structure
		createTempFile(t, tempDir, "file1.txt", "content1")
		createTempFile(t, tempDir, "subdir/file2.txt", "content2")
		createTempFile(t, tempDir, ".syncignore", "*.log\ntemp/")

		// Load ignore patterns
		ignorePatterns = nil
		err := loadSyncIgnoreFile()
		assert.NoError(t, err)

		// Create ignored files
		createTempFile(t, tempDir, "test.log", "should be ignored")
		createTempFile(t, tempDir, "temp/cache.txt", "should be ignored")

		// Verify ignore patterns work
		assert.True(t, shouldIgnore("*.log"))
		assert.True(t, shouldIgnore("temp/"))
		assert.False(t, shouldIgnore("file1.txt"))
		assert.False(t, shouldIgnore("subdir/file2.txt"))
	})

	t.Run("concurrent file operations", func(t *testing.T) {
		tempDir := t.TempDir()

		// Create multiple files
		for i := 0; i < 10; i++ {
			createTempFile(t, tempDir, fmt.Sprintf("file%d.txt", i), fmt.Sprintf("content%d", i))
		}

		// Verify all files exist
		files, err := filepath.Glob(filepath.Join(tempDir, "*.txt"))
		assert.NoError(t, err)
		assert.Len(t, files, 10)
	})

	t.Run("nested directory structure", func(t *testing.T) {
		tempDir := t.TempDir()

		// Create nested structure
		createTempFile(t, tempDir, "a/b/c/deep.txt", "deep content")
		createTempFile(t, tempDir, "x/y/z/file.txt", "another deep file")

		// Verify structure
		deepFile := filepath.Join(tempDir, "a", "b", "c", "deep.txt")
		assert.FileExists(t, deepFile)

		content, err := os.ReadFile(deepFile)
		assert.NoError(t, err)
		assert.Equal(t, "deep content", string(content))
	})
}

// Benchmark tests
func BenchmarkCalculateMD5Small(b *testing.B) {
	tempDir := b.TempDir()
	content := "small benchmark content"
	filePath := createTempFile(&testing.T{}, tempDir, "bench_small.txt", content)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = calculateMD5(filePath)
	}
}

func BenchmarkCalculateMD5Large(b *testing.B) {
	tempDir := b.TempDir()
	content := strings.Repeat("benchmark content ", 100000)
	filePath := createTempFile(&testing.T{}, tempDir, "bench_large.txt", content)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = calculateMD5(filePath)
	}
}

func BenchmarkShouldIgnore(b *testing.B) {
	ignorePatterns = []string{"*.log", "temp/", ".git/", "node_modules/", "build/"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		shouldIgnore("src/main.go")
	}
}

func BenchmarkShouldIgnoreMatch(b *testing.B) {
	ignorePatterns = []string{"*.log", "temp/", ".git/", "node_modules/", "build/"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		shouldIgnore("*.log")
	}
}
