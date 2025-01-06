package main

import (
	"bufio"
	"crypto/md5"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/robfig/cron/v3"
)

var (
	bucketName     = ""
	region         = ""
	rootDir        = ""
	ignorePatterns []string
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

	sess, err := session.NewSession(&aws.Config{
		Region: aws.String(region),
	})
	if err != nil {
		log.Fatalf("failed to create AWS session: %v", err)
	}

	s3Client := s3.New(sess)

	startScheduler(s3Client, cronSchedule)
}

func startScheduler(s3Client *s3.S3, cronSchedule string) {
	err := syncDirectoryWithS3(s3Client, rootDir)
	if err != nil {
		log.Printf("Sync failed: %v", err)
	} else {
		fmt.Println("Sync completed successfully.")
	}

	c := cron.New()
	_, err = c.AddFunc(cronSchedule, func() {
		fmt.Println("Running sync...")
		err := syncDirectoryWithS3(s3Client, rootDir)
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

func syncDirectoryWithS3(s3Client *s3.S3, root string) error {
	err := uploadDirectoryToS3(s3Client, root)
	if err != nil {
		return err
	}

	return deleteRemovedFilesFromS3(s3Client, root)
}

func uploadDirectoryToS3(s3Client *s3.S3, root string) error {
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
			size, err := uploadFileS3(s3Client, s3Key, path)
			if err != nil {
				return fmt.Errorf("failed to upload file to S3: %v", err)
			}
			fmt.Printf("%s uploaded (%d bytes)\n", path, size)
		} else {
			fmt.Printf("%s is up-to-date, skipping upload.\n", path)
		}
		return nil
	})

	return err
}

func deleteRemovedFilesFromS3(s3Client *s3.S3, root string) error {
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

func fileChangedOnS3(s3Client *s3.S3, s3Key, localPath string) (bool, error) {
	headObjectOutput, err := s3Client.HeadObject(&s3.HeadObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(s3Key),
	})
	if err != nil {
		if strings.Contains(err.Error(), "404") {

			return true, nil
		}
		return false, fmt.Errorf("error checking S3 object: %v", err)
	}

	localFileHash, err := calculateMD5(localPath)
	if err != nil {
		return false, fmt.Errorf("error calculating local file hash: %v", err)
	}

	s3ETag := strings.Trim(*headObjectOutput.ETag, "\"")

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

func uploadFileS3(s3Client *s3.S3, s3Key string, filePath string) (int64, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return 0, fmt.Errorf("failed to open file: %v", err)
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		return 0, fmt.Errorf("failed to get file info: %v", err)
	}

	fileSize := fileInfo.Size()

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
