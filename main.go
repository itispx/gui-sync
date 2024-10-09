package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
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

	err := loadSyncIgnoreFile()
	if err != nil {
		log.Fatalf("failed to load .syncignore file: %v", err)
	}

	err = uploadDirectoryToS3(rootDir)
	if err != nil {
		log.Printf("failed to upload files: %v", err)
	}
}

func uploadDirectoryToS3(root string) error {
	sess, err := session.NewSession(&aws.Config{
		Region: aws.String(region),
	})
	if err != nil {
		return fmt.Errorf("failed to create AWS session: %v", err)
	}

	s3Client := s3.New(sess)

	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
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

		exists := existsS3(s3Client, bucketName, s3Key)

		if !exists {
			size, err := uploadFileS3(s3Client, s3Key, path)
			if err != nil {
				return fmt.Errorf("failed to upload file to S3: %v", err)
			}

			fmt.Printf("%s uploaded (%d bytes)\n", path, size)
		}

		return nil
	})

	return err
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

func existsS3(client *s3.S3, bucket string, key string) bool {
	_, err := client.HeadObject(&s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})

	return err == nil
}

func uploadFileS3(s3Client *s3.S3, s3Key string, filePath string) (int64, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return 0, fmt.Errorf("failed to open file: %v", err)
	}
	defer file.Close()

	// Get file information
	fileInfo, err := file.Stat()
	if err != nil {
		return 0, fmt.Errorf("failed to get file info: %v", err)
	}

	// Get file size in bytes
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
