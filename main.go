package main

import (
	"bufio"
	"crypto/md5"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3iface"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/robfig/cron/v3"
)

var (
	bucketName     = ""
	region         = ""
	rootDir        = ""
	ignorePatterns []string
)

const (
	// Files larger than this threshold will use multipart upload
	multipartThreshold = 100 * 1024 * 1024 // 100 MB
	// Size of each part in multipart upload - increased for stability
	partSize = 50 * 1024 * 1024 // 50 MB (larger parts = fewer requests)
	// Number of files to upload concurrently
	uploadWorkers = 5
	// Number of parts to upload concurrently per file - reduced for stability
	partConcurrency = 3 // Reduced from 10 to 3
)

func main() {
	if len(os.Args) < 4 {
		log.Fatalln("not enough arguments.")
	}

	bucketName = os.Args[1]
	region = os.Args[2]
	rootDir = os.Args[3]
	cronSchedule := os.Args[4]

	err := loadSyncIgnoreFile()
	if err != nil {
		log.Fatalf("failed to load .syncignore file: %v", err)
	}

	// Create session with retry configuration
	sess, err := session.NewSession(&aws.Config{
		Region:     aws.String(region),
		MaxRetries: aws.Int(10), // Increase retries
		HTTPClient: &http.Client{
			Timeout: 300 * time.Second, // 5 minute timeout per request
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 100,
				IdleConnTimeout:     90 * time.Second,
				DisableKeepAlives:   false,
			},
		},
	})
	if err != nil {
		log.Fatalf("failed to create AWS session: %v", err)
	}

	// Add retry handlers
	sess.Handlers.Retry.PushBack(func(r *request.Request) {
		if r.Error != nil {
			log.Printf("Retry attempt %d for %s: %v", r.RetryCount, r.Operation.Name, r.Error)
		}
	})

	s3Client := s3.New(sess)

	startScheduler(s3Client, sess, cronSchedule)
}

func startScheduler(s3Client s3iface.S3API, sess *session.Session, cronSchedule string) {
	err := syncDirectoryWithS3(s3Client, sess, rootDir)
	if err != nil {
		log.Printf("Sync failed: %v", err)
	} else {
		fmt.Println("Sync completed successfully.")
	}

	c := cron.New()
	_, err = c.AddFunc(cronSchedule, func() {
		fmt.Println("Running sync...")
		err := syncDirectoryWithS3(s3Client, sess, rootDir)
		if err != nil {
			log.Printf("Sync failed: %v", err)
		} else {
			fmt.Println("Sync completed successfully.")
		}
	})
	if err != nil {
		log.Fatalf("Invalid cron schedule: %v", err)
	}

	fmt.Printf("Scheduler started with cron schedule: %s\n", cronSchedule)
	c.Start()

	select {}
}

func syncDirectoryWithS3(s3Client s3iface.S3API, sess *session.Session, root string) error {
	err := uploadDirectoryToS3(s3Client, sess, root)
	if err != nil {
		return err
	}

	return deleteRemovedFilesFromS3(s3Client, root)
}

func uploadDirectoryToS3(s3Client s3iface.S3API, sess *session.Session, root string) error {
	type uploadTask struct {
		path     string
		relPath  string
		s3Key    string
		fileSize int64
	}

	tasks := make(chan uploadTask, 100)
	var wg sync.WaitGroup
	var uploadErrors []error
	var errorMutex sync.Mutex

	// Start worker goroutines
	for i := 0; i < uploadWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for task := range tasks {
				size, err := uploadFileS3(s3Client, sess, task.s3Key, task.path, task.fileSize)
				if err != nil {
					errorMutex.Lock()
					uploadErrors = append(uploadErrors, fmt.Errorf("failed to upload %s: %v", task.path, err))
					errorMutex.Unlock()
				} else {
					fmt.Printf("[Worker %d] %s uploaded (%d bytes)\n", workerID, task.path, size)
				}
			}
		}(i)
	}

	// Walk directory and queue upload tasks
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}

		if runtime.GOOS == "windows" {
			relPath = strings.ReplaceAll(relPath, "\\", "/")
		}

		if shouldIgnore(relPath) {
			return nil
		}

		s3Key := relPath

		shouldUpload, err := fileChangedOnS3(s3Client, s3Key, path)
		if err != nil {
			return err
		}

		if shouldUpload {
			tasks <- uploadTask{
				path:     path,
				relPath:  relPath,
				s3Key:    s3Key,
				fileSize: info.Size(),
			}
		} else {
			fmt.Printf("%s is up-to-date, skipping upload.\n", path)
		}
		return nil
	})

	// Close tasks channel and wait for workers to finish
	close(tasks)
	wg.Wait()

	if err != nil {
		return err
	}

	// Check if any uploads failed
	if len(uploadErrors) > 0 {
		return fmt.Errorf("upload errors occurred: %v", uploadErrors)
	}

	return nil
}

func deleteRemovedFilesFromS3(s3Client s3iface.S3API, root string) error {
	var localFiles = make(map[string]bool)

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			relPath, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			if runtime.GOOS == "windows" {
				relPath = strings.ReplaceAll(relPath, "\\", "/")
			}
			localFiles[relPath] = true
		}
		return nil
	})
	if err != nil {
		return err
	}

	err = s3Client.ListObjectsV2Pages(&s3.ListObjectsV2Input{
		Bucket: aws.String(bucketName),
	}, func(page *s3.ListObjectsV2Output, lastPage bool) bool {
		for _, obj := range page.Contents {
			if _, exists := localFiles[*obj.Key]; !exists {
				_, err := s3Client.DeleteObject(&s3.DeleteObjectInput{
					Bucket: aws.String(bucketName),
					Key:    obj.Key,
				})
				if err == nil {
					fmt.Printf("%s deleted from S3\n", *obj.Key)
				}
			}
		}
		return true
	})
	if err != nil {
		return fmt.Errorf("failed to delete files from S3: %v", err)
	}

	return nil
}

func fileChangedOnS3(s3Client s3iface.S3API, s3Key, localPath string) (bool, error) {
	headObjectOutput, err := s3Client.HeadObject(&s3.HeadObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(s3Key),
	})
	if err != nil {
		if aerr, ok := err.(awserr.RequestFailure); ok && aerr.StatusCode() == http.StatusNotFound {
			return true, nil
		}
		return false, fmt.Errorf("error checking S3 object: %v", err)
	}

	// Get local file info
	fileInfo, err := os.Stat(localPath)
	if err != nil {
		return false, fmt.Errorf("failed to stat local file: %v", err)
	}

	// First check: compare file sizes
	if *headObjectOutput.ContentLength != fileInfo.Size() {
		return true, nil
	}

	// Second check: compare last modified times
	// If S3 file is newer or same age as local file, skip upload
	if headObjectOutput.LastModified == nil {
		return true, nil
	}

	if headObjectOutput.LastModified != nil && !fileInfo.ModTime().After(*headObjectOutput.LastModified) {
		return false, nil
	}

	// For large files, skip expensive MD5 calculation
	if fileInfo.Size() > multipartThreshold {
		// Rely on size + modification time for large files
		return fileInfo.ModTime().After(*headObjectOutput.LastModified), nil
	}

	// For smaller files, do MD5 comparison
	localFileHash, err := calculateMD5(localPath)
	if err != nil {
		return false, fmt.Errorf("error calculating local file hash: %v", err)
	}

	s3ETag := strings.Trim(*headObjectOutput.ETag, "\"")

	// ETags for multipart uploads contain a dash (e.g., "abc123-5")
	// In this case, we can't do MD5 comparison
	if strings.Contains(s3ETag, "-") {
		// Multipart upload ETag - rely on size and time
		return fileInfo.ModTime().After(*headObjectOutput.LastModified), nil
	}

	return localFileHash != s3ETag, nil
}

func calculateMD5(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to open file: %v", err)
	}
	defer file.Close()

	hash := md5.New()
	_, err = io.Copy(hash, file)
	if err != nil {
		return "", fmt.Errorf("failed to hash file: %v", err)
	}

	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

func loadSyncIgnoreFile() error {
	file, err := os.Open(filepath.Join(rootDir, ".syncignore"))
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Printf("no .syncignore file found, proceeding without ignoring files...\n")
			return nil
		}
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		ignorePatterns = append(ignorePatterns, line)
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading .syncignore file: %v", err)
	}

	fmt.Printf("loaded ignore patterns: %v\n", ignorePatterns)

	return nil
}

func shouldIgnore(path string) bool {
	for _, pattern := range ignorePatterns {
		if pattern == path {
			return true
		}
	}

	return false
}

func uploadFileS3(s3Client s3iface.S3API, sess *session.Session, s3Key string, filePath string, fileSize int64) (int64, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return 0, fmt.Errorf("failed to open file: %v", err)
	}
	defer file.Close()

	// Use multipart upload for large files
	if fileSize > multipartThreshold {
		fmt.Printf("Using multipart upload for %s (size: %d bytes)\n", filePath, fileSize)
		return uploadMultipart(sess, s3Key, file, fileSize)
	}

	// Use standard upload for smaller files
	_, err = s3Client.PutObject(&s3.PutObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(s3Key),
		Body:   file,
	})
	if err != nil {
		return 0, fmt.Errorf("failed to upload file to S3: %v", err)
	}

	return fileSize, nil
}

func uploadMultipart(sess *session.Session, s3Key string, file *os.File, fileSize int64) (int64, error) {
	// Reset file pointer to beginning
	_, err := file.Seek(0, 0)
	if err != nil {
		return 0, fmt.Errorf("failed to reset file pointer: %v", err)
	}

	uploader := s3manager.NewUploader(sess, func(u *s3manager.Uploader) {
		u.PartSize = partSize
		u.Concurrency = partConcurrency
		u.MaxUploadParts = 10000
		u.LeavePartsOnError = false
	})

	// Upload with retries
	_, err = uploader.Upload(&s3manager.UploadInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(s3Key),
		Body:   file,
	})
	if err != nil {
		return 0, fmt.Errorf("failed to upload file via multipart: %v", err)
	}

	return fileSize, nil
}
