package main

import (
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Integration test configuration
const (
	testBucketName = "gui-sync-test" // Change this to your test bucket
	testRegion     = "us-east-1"     // Change this to your region
)

// createFileWithSize creates a file of specified size filled with random data
func createFileWithSize(t *testing.T, dir, name string, sizeBytes int64) string {
	path := filepath.Join(dir, name)
	err := os.MkdirAll(filepath.Dir(path), 0755)
	require.NoError(t, err)

	file, err := os.Create(path)
	require.NoError(t, err)
	defer file.Close()

	// Write random data in chunks to avoid memory issues
	const chunkSize = 10 * 1024 * 1024 // 10MB chunks
	written := int64(0)
	buf := make([]byte, chunkSize)

	for written < sizeBytes {
		remaining := sizeBytes - written
		writeSize := chunkSize
		if remaining < int64(chunkSize) {
			writeSize = int(remaining)
			buf = buf[:writeSize]
		}

		// Fill buffer with random data
		_, err := rand.Read(buf)
		require.NoError(t, err)

		n, err := file.Write(buf)
		require.NoError(t, err)
		written += int64(n)

		// Print progress for large files
		if sizeBytes > 1024*1024*1024 { // > 1GB
			if written%(1024*1024*1024) == 0 || written == sizeBytes {
				t.Logf("Created %d/%d GB of %s", written/(1024*1024*1024), sizeBytes/(1024*1024*1024), name)
			}
		}
	}

	return path
}

// createSparseFile creates a sparse file (doesn't actually allocate disk space)
// Useful for testing 50GB without using disk space
func createSparseFile(t *testing.T, dir, name string, sizeBytes int64) string {
	path := filepath.Join(dir, name)
	err := os.MkdirAll(filepath.Dir(path), 0755)
	require.NoError(t, err)

	file, err := os.Create(path)
	require.NoError(t, err)
	defer file.Close()

	// Seek to the desired size - 1 and write one byte
	// This creates a sparse file on most filesystems
	_, err = file.Seek(sizeBytes-1, 0)
	require.NoError(t, err)

	_, err = file.Write([]byte{0})
	require.NoError(t, err)

	return path
}

// setupS3Client creates a real S3 client for integration tests
func setupS3Client(t *testing.T) (*s3.S3, *session.Session) {
	sess, err := session.NewSession(&aws.Config{
		Region: aws.String(testRegion),
	})
	require.NoError(t, err)

	client := s3.New(sess)

	return client, sess
}

// cleanupS3Objects deletes test objects from S3
func cleanupS3Objects(t *testing.T, client *s3.S3, keys []string) {
	for _, key := range keys {
		_, err := client.DeleteObject(&s3.DeleteObjectInput{
			Bucket: aws.String(testBucketName),
			Key:    aws.String(key),
		})
		if err != nil {
			t.Logf("Warning: failed to cleanup %s: %v", key, err)
		}
	}
}

// TestIntegrationS3Upload tests uploading various file sizes to S3
// Run with: go test -v -run TestIntegrationS3Upload -tags=integration
func TestIntegrationS3Upload(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Save original bucket name
	originalBucket := bucketName
	defer func() {
		bucketName = originalBucket
	}()
	bucketName = testBucketName

	client, sess := setupS3Client(t)
	tempDir := t.TempDir()

	testCases := []struct {
		name      string
		filename  string
		size      int64
		useSparse bool
	}{
		{
			name:      "1KB file",
			filename:  "test-1kb.dat",
			size:      1024,
			useSparse: false,
		},
		{
			name:      "1MB file",
			filename:  "test-1mb.dat",
			size:      1024 * 1024,
			useSparse: false,
		},
		{
			name:      "10MB file",
			filename:  "test-10mb.dat",
			size:      10 * 1024 * 1024,
			useSparse: false,
		},
		{
			name:      "100MB file",
			filename:  "test-100mb.dat",
			size:      100 * 1024 * 1024,
			useSparse: false,
		},
		{
			name:      "1GB file",
			filename:  "test-1gb.dat",
			size:      1024 * 1024 * 1024,
			useSparse: false,
		},
	}

	uploadedKeys := make([]string, 0)
	defer func() {
		cleanupS3Objects(t, client, uploadedKeys)
	}()

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var filePath string

			t.Logf("Creating %s (%d bytes)...", tc.filename, tc.size)
			startCreate := time.Now()

			if tc.useSparse {
				filePath = createSparseFile(t, tempDir, tc.filename, tc.size)
			} else {
				filePath = createFileWithSize(t, tempDir, tc.filename, tc.size)
			}

			createDuration := time.Since(startCreate)
			t.Logf("File created in %v", createDuration)

			// Verify file size
			fileInfo, err := os.Stat(filePath)
			require.NoError(t, err)
			assert.Equal(t, tc.size, fileInfo.Size())

			// Upload to S3
			t.Logf("Uploading %s to S3...", tc.filename)
			startUpload := time.Now()

			uploadSize, err := uploadFileS3(client, sess, tc.filename, filePath, tc.size)
			require.NoError(t, err)
			assert.Equal(t, tc.size, uploadSize)

			uploadDuration := time.Since(startUpload)
			t.Logf("Upload completed in %v (%.2f MB/s)",
				uploadDuration,
				float64(tc.size)/(1024*1024)/uploadDuration.Seconds())

			uploadedKeys = append(uploadedKeys, tc.filename)

			// Verify file exists on S3
			headOutput, err := client.HeadObject(&s3.HeadObjectInput{
				Bucket: aws.String(testBucketName),
				Key:    aws.String(tc.filename),
			})
			require.NoError(t, err)
			assert.Equal(t, tc.size, *headOutput.ContentLength)
		})
	}
}

// TestIntegration50GBUpload tests uploading a 50GB file to S3
// This uses a sparse file to avoid using 50GB of actual disk space
// Run with: go test -v -run TestIntegration50GBUpload -tags=integration -timeout=2h
func TestIntegration50GBUpload(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Check for explicit flag to run this expensive test
	if os.Getenv("RUN_50GB_TEST") != "true" {
		t.Skip("Skipping 50GB test. Set RUN_50GB_TEST=true to run this test")
	}

	// Save original bucket name
	originalBucket := bucketName
	defer func() {
		bucketName = originalBucket
	}()
	bucketName = testBucketName

	client, sess := setupS3Client(t)
	tempDir := t.TempDir()

	const (
		filename = "test-50gb.dat"
		size50GB = 50 * 1024 * 1024 * 1024 // 50GB
	)

	t.Logf("Creating 50GB sparse file...")
	startCreate := time.Now()
	filePath := createSparseFile(t, tempDir, filename, size50GB)
	t.Logf("Sparse file created in %v", time.Since(startCreate))

	// Verify file size
	fileInfo, err := os.Stat(filePath)
	require.NoError(t, err)
	assert.Equal(t, int64(size50GB), fileInfo.Size())

	// Upload to S3
	t.Logf("Starting 50GB upload to S3...")
	t.Logf("This may take 30+ minutes depending on your connection...")
	startUpload := time.Now()

	uploadSize, err := uploadFileS3(client, sess, filename, filePath, size50GB)
	require.NoError(t, err)
	assert.Equal(t, int64(size50GB), uploadSize)

	uploadDuration := time.Since(startUpload)
	t.Logf("50GB upload completed in %v (%.2f MB/s)",
		uploadDuration,
		float64(size50GB)/(1024*1024)/uploadDuration.Seconds())

	// Cleanup
	defer func() {
		t.Logf("Cleaning up 50GB test file from S3...")
		cleanupS3Objects(t, client, []string{filename})
	}()

	// Verify file exists on S3
	headOutput, err := client.HeadObject(&s3.HeadObjectInput{
		Bucket: aws.String(testBucketName),
		Key:    aws.String(filename),
	})
	require.NoError(t, err)
	assert.Equal(t, int64(size50GB), *headOutput.ContentLength)

	// Verify it's a multipart upload (ETag will contain a dash)
	assert.Contains(t, *headOutput.ETag, "-", "Expected multipart upload ETag format")
	t.Logf("Multipart upload confirmed: ETag=%s", *headOutput.ETag)
}

// TestIntegrationMultipleFilesUpload tests uploading multiple files concurrently
func TestIntegrationMultipleFilesUpload(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Save original bucket name
	originalBucket := bucketName
	defer func() {
		bucketName = originalBucket
	}()
	bucketName = testBucketName

	client, sess := setupS3Client(t)
	tempDir := t.TempDir()

	// Create multiple files of different sizes
	files := []struct {
		name string
		size int64
	}{
		{"file1.dat", 5 * 1024 * 1024},      // 5MB
		{"file2.dat", 10 * 1024 * 1024},     // 10MB
		{"file3.dat", 25 * 1024 * 1024},     // 25MB
		{"dir/file4.dat", 50 * 1024 * 1024}, // 50MB in subdirectory
	}

	uploadedKeys := make([]string, 0)
	defer func() {
		cleanupS3Objects(t, client, uploadedKeys)
	}()

	t.Logf("Creating and uploading %d files...", len(files))
	startTotal := time.Now()

	for _, f := range files {
		filePath := createFileWithSize(t, tempDir, f.name, f.size)

		uploadSize, err := uploadFileS3(client, sess, f.name, filePath, f.size)
		require.NoError(t, err)
		assert.Equal(t, f.size, uploadSize)

		uploadedKeys = append(uploadedKeys, f.name)
		t.Logf("Uploaded %s (%d bytes)", f.name, f.size)
	}

	totalDuration := time.Since(startTotal)
	t.Logf("All files uploaded in %v", totalDuration)

	// Verify all files exist on S3
	for _, f := range files {
		headOutput, err := client.HeadObject(&s3.HeadObjectInput{
			Bucket: aws.String(testBucketName),
			Key:    aws.String(f.name),
		})
		require.NoError(t, err)
		assert.Equal(t, f.size, *headOutput.ContentLength)
	}
}

// TestIntegrationFileChangedDetection tests the file change detection with real S3
func TestIntegrationFileChangedDetection(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Save original bucket name
	originalBucket := bucketName
	defer func() {
		bucketName = originalBucket
	}()
	bucketName = testBucketName

	client, sess := setupS3Client(t)
	tempDir := t.TempDir()

	filename := "test-change-detection.txt"
	content := "initial content"
	filePath := createTempFile(t, tempDir, filename, content)

	defer cleanupS3Objects(t, client, []string{filename})

	// Upload initial file
	_, err := uploadFileS3(client, sess, filename, filePath, int64(len(content)))
	require.NoError(t, err)

	// Test 1: File hasn't changed
	t.Run("file unchanged", func(t *testing.T) {
		changed, err := fileChangedOnS3(client, filename, filePath)
		require.NoError(t, err)
		assert.False(t, changed, "File should not be detected as changed")
	})

	// Test 2: Modify file content
	t.Run("file content changed", func(t *testing.T) {
		time.Sleep(2 * time.Second) // Ensure timestamp difference
		newContent := "modified content that is different"
		err := os.WriteFile(filePath, []byte(newContent), 0644)
		require.NoError(t, err)

		changed, err := fileChangedOnS3(client, filename, filePath)
		require.NoError(t, err)
		assert.True(t, changed, "File should be detected as changed")
	})

	// Test 3: File doesn't exist on S3
	t.Run("new file", func(t *testing.T) {
		newFilePath := createTempFile(t, tempDir, "new-file.txt", "new content")
		changed, err := fileChangedOnS3(client, "new-file.txt", newFilePath)
		require.NoError(t, err)
		assert.True(t, changed, "New file should be detected as changed")
	})
}
