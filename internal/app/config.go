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

func LoadConfig() Config {
	h := getEnv("SAFEBIN_HOST", "0.0.0.0")
	p := getEnvInt("SAFEBIN_PORT", 8080)
	s := getEnv("SAFEBIN_STORAGE", "./storage")
	mDefault := int64(getEnvInt("SAFEBIN_MAX_MB", 512))

	var m int64
	flag.StringVar(&h, "h", h, "Bind address")
	flag.IntVar(&p, "p", p, "Port")
	flag.StringVar(&s, "s", s, "Storage directory")
	flag.Int64Var(&m, "m", mDefault, "Max file size in MB")
	flag.Parse()

	return Config{Addr: fmt.Sprintf("%s:%d", h, p), StorageDir: s, MaxMB: m}
}

func getEnv(k, f string) string {
	if v, ok := os.LookupEnv(k); ok {
		return v
	}
	return f
}

func getEnvInt(k string, f int) int {
	if v, ok := os.LookupEnv(k); ok {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return f
}

func ParseTemplates() *template.Template {
	return template.Must(template.ParseGlob("./web/templates/*.html"))
}
