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
	multipartThreshold = 100 * 1024 * 1024
	partSize           = 50 * 1024 * 1024
	uploadWorkers      = 5
	partConcurrency    = 3
)

func main() {
	fmt.Println("=== Sincronizador S3 ===")

	execPath, err := os.Executable()
	if err == nil {
		execName := filepath.Base(execPath)
		ignorePatterns = append(ignorePatterns, execName)
		fmt.Printf("✓ Executável será ignorado: %s\n\n", execName)
	}

	reader := bufio.NewReader(os.Stdin)

	fmt.Print("Digite o nome do bucket S3: ")
	bucketName, _ = reader.ReadString('\n')
	bucketName = strings.TrimSpace(bucketName)
	if bucketName == "" {
		log.Fatalln("Nome do bucket não pode estar vazio.")
	}

	fmt.Print("Digite a região AWS (ex: us-east-1): ")
	region, _ = reader.ReadString('\n')
	region = strings.TrimSpace(region)
	if region == "" {
		log.Fatalln("Região não pode estar vazia.")
	}

	fmt.Print("Digite o caminho do diretório a ser sincronizado: ")
	rootDir, _ = reader.ReadString('\n')
	rootDir = strings.TrimSpace(rootDir)
	if rootDir == "" {
		log.Fatalln("Diretório não pode estar vazio.")
	}

	if _, err := os.Stat(rootDir); os.IsNotExist(err) {
		log.Fatalf("Diretório não existe: %s", rootDir)
	}

	fmt.Print("Digite o agendamento cron (ex: */5 * * * * para cada 5 minutos): ")
	cronSchedule, _ := reader.ReadString('\n')
	cronSchedule = strings.TrimSpace(cronSchedule)
	if cronSchedule == "" {
		log.Fatalln("Agendamento cron não pode estar vazio.")
	}

	fmt.Println("\n--- Configurações ---")
	fmt.Printf("Bucket S3: %s\n", bucketName)
	fmt.Printf("Região AWS: %s\n", region)
	fmt.Printf("Diretório: %s\n", rootDir)
	fmt.Printf("Sincronização: %s\n", cronSchedule)
	fmt.Println("---------------------")

	err = loadSyncIgnoreFile()
	if err != nil {
		log.Fatalf("❌ Falha ao carregar arquivo .syncignore: %v", err)
	}

	fmt.Println("Conectando ao AWS S3...")

	sess, err := session.NewSession(&aws.Config{
		Region:     aws.String(region),
		MaxRetries: aws.Int(10),
		HTTPClient: &http.Client{
			Timeout: 300 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 100,
				IdleConnTimeout:     90 * time.Second,
				DisableKeepAlives:   false,
			},
		},
	})
	if err != nil {
		log.Fatalf("❌ Falha ao criar sessão AWS: %v", err)
	}

	fmt.Println("✓ Conectado ao AWS S3")

	sess.Handlers.Retry.PushBack(func(r *request.Request) {
		if r.Error != nil && r.RetryCount > 3 {
			log.Printf("⚠ Tentativa %d para %s", r.RetryCount, r.Operation.Name)
		}
	})

	s3Client := s3.New(sess)

	startScheduler(s3Client, sess, cronSchedule)
}

func startScheduler(s3Client s3iface.S3API, sess *session.Session, cronSchedule string) {
	fmt.Println("🔄 Iniciando primeira sincronização...")
	err := syncDirectoryWithS3(s3Client, sess, rootDir)
	if err != nil {
		log.Printf("❌ Sincronização falhou: %v", err)
	} else {
		fmt.Println("✓ Sincronização inicial concluída")
	}

	c := cron.New()
	_, err = c.AddFunc(cronSchedule, func() {
		fmt.Printf("\n🔄 [%s] Sincronizando...\n", time.Now().Format("15:04:05"))
		err := syncDirectoryWithS3(s3Client, sess, rootDir)
		if err != nil {
			log.Printf("❌ Sincronização falhou: %v", err)
		} else {
			fmt.Printf("✓ [%s] Sincronização concluída\n", time.Now().Format("15:04:05"))
		}
	})
	if err != nil {
		log.Fatalf("❌ Agendamento cron inválido: %v", err)
	}

	fmt.Printf("⏰ Agendador ativo (executa %s)\n", cronSchedule)
	fmt.Println("Pressione Ctrl+C para parar")
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
					uploadErrors = append(uploadErrors, fmt.Errorf("falha ao fazer upload de %s: %v", task.path, err))
					errorMutex.Unlock()
					log.Printf("  ❌ %s - %v", task.relPath, err)
				} else {
					fmt.Printf("  ✓ %s (%d bytes)\n", task.relPath, size)
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
			fmt.Printf("  ⏭ %s (sincronizado)\n", relPath)
		}
		return nil
	})

	close(tasks)
	wg.Wait()

	if err != nil {
		return err
	}

	if len(uploadErrors) > 0 {
		return fmt.Errorf("erros de upload ocorreram: %v", uploadErrors)
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
					fmt.Printf("  🗑 %s (removido do S3)\n", *obj.Key)
				}
			}
		}
		return true
	})
	if err != nil {
		return fmt.Errorf("falha ao deletar arquivos do S3: %v", err)
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
		return false, fmt.Errorf("erro ao verificar objeto S3: %v", err)
	}

	fileInfo, err := os.Stat(localPath)
	if err != nil {
		return false, fmt.Errorf("falha ao obter informações do arquivo local: %v", err)
	}

	if *headObjectOutput.ContentLength != fileInfo.Size() {
		return true, nil
	}

	if headObjectOutput.LastModified == nil {
		return true, nil
	}

	if headObjectOutput.LastModified != nil && !fileInfo.ModTime().After(*headObjectOutput.LastModified) {
		return false, nil
	}

	if fileInfo.Size() > multipartThreshold {
		return fileInfo.ModTime().After(*headObjectOutput.LastModified), nil
	}

	localFileHash, err := calculateMD5(localPath)
	if err != nil {
		return false, fmt.Errorf("erro ao calcular hash do arquivo local: %v", err)
	}

	s3ETag := strings.Trim(*headObjectOutput.ETag, "\"")

	if strings.Contains(s3ETag, "-") {
		return fileInfo.ModTime().After(*headObjectOutput.LastModified), nil
	}

	return localFileHash != s3ETag, nil
}

func calculateMD5(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("falha ao abrir arquivo: %v", err)
	}
	defer file.Close()

	hash := md5.New()
	_, err = io.Copy(hash, file)
	if err != nil {
		return "", fmt.Errorf("falha ao gerar hash do arquivo: %v", err)
	}

	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

func loadSyncIgnoreFile() error {
	file, err := os.Open(filepath.Join(rootDir, ".syncignore"))
	if err != nil {
		if os.IsNotExist(err) {
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
		return fmt.Errorf("erro ao ler arquivo .syncignore: %v", err)
	}

	fmt.Printf("✓ Arquivo .syncignore carregado (%d padrões)\n", len(ignorePatterns))

	return nil
}

func shouldIgnore(path string) bool {
	fileName := filepath.Base(path)

	for _, pattern := range ignorePatterns {
		if pattern == path {
			return true
		}

		if pattern == fileName {
			return true
		}
	}

	return false
}

func uploadFileS3(s3Client s3iface.S3API, sess *session.Session, s3Key string, filePath string, fileSize int64) (int64, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return 0, fmt.Errorf("falha ao abrir arquivo: %v", err)
	}
	defer file.Close()

	if fileSize > multipartThreshold {
		fmt.Printf("  📦 Upload multipart: %s (%.2f MB)\n", filepath.Base(filePath), float64(fileSize)/(1024*1024))
		return uploadMultipart(sess, s3Key, file, fileSize)
	}

	_, err = s3Client.PutObject(&s3.PutObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(s3Key),
		Body:   file,
	})
	if err != nil {
		return 0, fmt.Errorf("falha ao fazer upload do arquivo para S3: %v", err)
	}

	return fileSize, nil
}

func uploadMultipart(sess *session.Session, s3Key string, file *os.File, fileSize int64) (int64, error) {
	_, err := file.Seek(0, 0)
	if err != nil {
		return 0, fmt.Errorf("falha ao resetar ponteiro do arquivo: %v", err)
	}

	uploader := s3manager.NewUploader(sess, func(u *s3manager.Uploader) {
		u.PartSize = partSize
		u.Concurrency = partConcurrency
		u.MaxUploadParts = 10000
		u.LeavePartsOnError = false
	})

	_, err = uploader.Upload(&s3manager.UploadInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(s3Key),
		Body:   file,
	})
	if err != nil {
		return 0, fmt.Errorf("falha ao fazer upload do arquivo via multipart: %v", err)
	}

	return fileSize, nil
}
