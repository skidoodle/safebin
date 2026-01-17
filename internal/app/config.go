package app

import (
	"flag"
	"fmt"
	"html/template"
	"log/slog"
	"os"
	"strconv"
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
}

const (
	defaultHost = "0.0.0.0"
	defaultPort  = 8080
	defaultStorage = "./storage"
	defaultMaxMB = 512
)

func LoadConfig() Config {
	hostEnv := getEnv("SAFEBIN_HOST", defaultHost)
	portEnv := getEnvInt("SAFEBIN_PORT", defaultPort)
	storageEnv := getEnv("SAFEBIN_STORAGE", defaultStorage)
	maxMBEnv := int64(getEnvInt("SAFEBIN_MAX_MB", defaultMaxMB))

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

func ParseTemplates() *template.Template {
	return template.Must(template.ParseGlob("./web/templates/*.html"))
}
