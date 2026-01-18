package app

import (
	"flag"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"os"
	"strconv"
	"time"

	"go.etcd.io/bbolt"
)

const (
	DefaultHost     = "0.0.0.0"
	DefaultPort     = 8080
	DefaultStorage  = "./storage"
	DefaultMaxMB    = 512
	ServerTimeout   = 10 * time.Minute
	ShutdownTimeout = 10 * time.Second

	UploadChunkSize    = 8 << 20
	MinChunkSize       = 1 << 20
	MaxRequestOverhead = 10 << 20
	PermUserRWX        = 0o700
	MegaByte           = 1 << 20
	ChunkSafetyMargin  = 2

	SlugLength = 22
	KeyLength  = 16

	CleanupInterval = 1 * time.Hour
	TempExpiry      = 4 * time.Hour
	MinRetention    = 24 * time.Hour
	MaxRetention    = 365 * 24 * time.Hour

	DBDirName         = "db"
	DBFileName        = "safebin.db"
	DBBucketName      = "files"
	DBBucketIndexName = "expiry_index"
	TempDirName       = "tmp"
)

type Config struct {
	Addr       string
	StorageDir string
	MaxMB      int64
}

type App struct {
	Conf   Config
	Tmpl   *template.Template
	Logger *slog.Logger
	DB     *bbolt.DB
	Assets fs.FS
}

func LoadConfig() Config {
	hostEnv := getEnv("SAFEBIN_HOST", DefaultHost)
	portEnv := getEnvInt("SAFEBIN_PORT", DefaultPort)
	storageEnv := getEnv("SAFEBIN_STORAGE", DefaultStorage)
	maxMBEnv := int64(getEnvInt("SAFEBIN_MAX_MB", DefaultMaxMB))

	var host string
	var port int
	var storage string
	var maxMB int64

	flag.StringVar(&host, "h", hostEnv, "Bind address")
	flag.IntVar(&port, "p", portEnv, "Port")
	flag.StringVar(&storage, "s", storageEnv, "Storage directory")
	flag.Int64Var(&maxMB, "m", maxMBEnv, "Max file size in MB")
	flag.Parse()

	return Config{
		Addr:       fmt.Sprintf("%s:%d", host, port),
		StorageDir: storage,
		MaxMB:      maxMB,
	}
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if value, ok := os.LookupEnv(key); ok {
		i, err := strconv.Atoi(value)
		if err == nil {
			return i
		}
	}
	return fallback
}

func ParseTemplates(fsys fs.FS) *template.Template {
	return template.Must(template.ParseFS(fsys, "*.html"))
}
