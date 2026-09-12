// Package config loads validated configuration from an optional JSON file and
// environment variables. Environment variables always take precedence.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	HTTPAddr          string        `json:"http_addr"`
	DatabaseURL       string        `json:"database_url"`
	DataDir           string        `json:"data_dir"`
	BackupDir         string        `json:"backup_dir"`
	Timezone          string        `json:"timezone"`
	LogLevel          string        `json:"log_level"`
	DBMaxConns        int32         `json:"db_max_conns"`
	DBMinConns        int32         `json:"db_min_conns"`
	CookieSecure      bool          `json:"cookie_secure"`
	SessionIdle       time.Duration `json:"-"`
	SessionAbsolute   time.Duration `json:"-"`
	SessionIdleText   string        `json:"session_idle"`
	SessionAbsText    string        `json:"session_absolute"`
	ShutdownTimeout   time.Duration `json:"-"`
	ShutdownText      string        `json:"shutdown_timeout"`
	PasswordHashLimit time.Duration `json:"-"`
	PasswordHashText  string        `json:"password_hash_limit"`
	MaxUploadBytes    int64         `json:"max_upload_bytes"`
	TurnstileSiteKey  string        `json:"turnstile_site_key"`
	TurnstileSecret   string        `json:"turnstile_secret"`
	TrustedProxyCIDRs []string      `json:"trusted_proxy_cidrs"`
}

func defaults() Config {
	return Config{
		HTTPAddr:         "127.0.0.1:8080",
		DataDir:          "/var/lib/forms",
		BackupDir:        "/var/backups/forms",
		Timezone:         "Asia/Tehran",
		LogLevel:         "info",
		DBMaxConns:       6,
		DBMinConns:       0,
		CookieSecure:     true,
		SessionIdleText:  "12h",
		SessionAbsText:   "168h",
		ShutdownText:     "15s",
		PasswordHashText: "2s",
		MaxUploadBytes:   100 << 20,
	}
}

func Load() (Config, error) {
	config := defaults()
	if path := strings.TrimSpace(os.Getenv("FORMS_CONFIG_FILE")); path != "" {
		file, err := os.Open(path)
		if err != nil {
			return Config{}, fmt.Errorf("open config file: %w", err)
		}
		decoder := json.NewDecoder(io.LimitReader(file, 1<<20))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&config); err != nil {
			file.Close()
			return Config{}, fmt.Errorf("decode config file: %w", err)
		}
		if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
			file.Close()
			return Config{}, errors.New("decode config file: file must contain exactly one JSON object")
		}
		if err := file.Close(); err != nil {
			return Config{}, fmt.Errorf("close config file: %w", err)
		}
	}

	applyString(&config.HTTPAddr, "FORMS_HTTP_ADDR")
	applyString(&config.DatabaseURL, "FORMS_DATABASE_URL")
	applyString(&config.DataDir, "FORMS_DATA_DIR")
	applyString(&config.BackupDir, "FORMS_BACKUP_DIR")
	applyString(&config.Timezone, "FORMS_TIMEZONE")
	applyString(&config.LogLevel, "FORMS_LOG_LEVEL")
	applyString(&config.SessionIdleText, "FORMS_SESSION_IDLE")
	applyString(&config.SessionAbsText, "FORMS_SESSION_ABSOLUTE")
	applyString(&config.ShutdownText, "FORMS_SHUTDOWN_TIMEOUT")
	applyString(&config.PasswordHashText, "FORMS_PASSWORD_HASH_LIMIT")
	applyString(&config.TurnstileSiteKey, "FORMS_TURNSTILE_SITE_KEY")
	applyString(&config.TurnstileSecret, "FORMS_TURNSTILE_SECRET")
	if value, ok := os.LookupEnv("FORMS_TRUSTED_PROXY_CIDRS"); ok {
		config.TrustedProxyCIDRs = splitCSV(value)
	}

	var err error
	if config.DBMaxConns, err = envInt32("FORMS_DB_MAX_CONNS", config.DBMaxConns); err != nil {
		return Config{}, err
	}
	if config.DBMinConns, err = envInt32("FORMS_DB_MIN_CONNS", config.DBMinConns); err != nil {
		return Config{}, err
	}
	if config.MaxUploadBytes, err = envInt64("FORMS_MAX_UPLOAD_BYTES", config.MaxUploadBytes); err != nil {
		return Config{}, err
	}
	if value, ok := os.LookupEnv("FORMS_INSECURE_COOKIE"); ok {
		insecure, parseErr := strconv.ParseBool(value)
		if parseErr != nil {
			return Config{}, fmt.Errorf("FORMS_INSECURE_COOKIE: %w", parseErr)
		}
		config.CookieSecure = !insecure
	}

	if config.SessionIdle, err = time.ParseDuration(config.SessionIdleText); err != nil {
		return Config{}, fmt.Errorf("session_idle: %w", err)
	}
	if config.SessionAbsolute, err = time.ParseDuration(config.SessionAbsText); err != nil {
		return Config{}, fmt.Errorf("session_absolute: %w", err)
	}
	if config.ShutdownTimeout, err = time.ParseDuration(config.ShutdownText); err != nil {
		return Config{}, fmt.Errorf("shutdown_timeout: %w", err)
	}
	if config.PasswordHashLimit, err = time.ParseDuration(config.PasswordHashText); err != nil {
		return Config{}, fmt.Errorf("password_hash_limit: %w", err)
	}
	if err := config.Validate(); err != nil {
		return Config{}, err
	}
	return config, nil
}

func (c Config) Validate() error {
	var problems []string
	if strings.TrimSpace(c.DatabaseURL) == "" {
		problems = append(problems, "FORMS_DATABASE_URL الزامی است")
	}
	if host, _, err := net.SplitHostPort(c.HTTPAddr); err != nil {
		problems = append(problems, "FORMS_HTTP_ADDR باید host:port معتبر باشد")
	} else if host == "" {
		problems = append(problems, "گوش‌دادن روی همهٔ interfaceها باید صریح باشد")
	}
	if c.DBMaxConns < 1 || c.DBMaxConns > 32 {
		problems = append(problems, "FORMS_DB_MAX_CONNS باید بین ۱ و ۳۲ باشد")
	}
	if c.DBMinConns < 0 || c.DBMinConns > c.DBMaxConns {
		problems = append(problems, "FORMS_DB_MIN_CONNS نامعتبر است")
	}
	if c.SessionIdle <= 0 || c.SessionAbsolute <= c.SessionIdle {
		problems = append(problems, "مهلت مطلق نشست باید از مهلت بیکاری بزرگ‌تر باشد")
	}
	if c.PasswordHashLimit < 100*time.Millisecond {
		problems = append(problems, "password_hash_limit بیش از حد کوتاه است")
	}
	if c.MaxUploadBytes < 1<<20 || c.MaxUploadBytes > 1<<30 {
		problems = append(problems, "FORMS_MAX_UPLOAD_BYTES باید بین ۱ MiB و ۱ GiB باشد")
	}
	if (c.TurnstileSiteKey == "") != (c.TurnstileSecret == "") {
		problems = append(problems, "کلید سایت و رمز Turnstile باید با هم تنظیم شوند")
	} else if c.TurnstileSiteKey != "" && (len(c.TurnstileSiteKey) < 8 || len(c.TurnstileSecret) < 8) {
		problems = append(problems, "کلیدهای Turnstile معتبر نیستند")
	}
	if _, err := time.LoadLocation(c.Timezone); err != nil {
		problems = append(problems, "timezone معتبر نیست")
	}
	trustedProxyPrefixes := make(map[netip.Prefix]struct{}, len(c.TrustedProxyCIDRs))
	for _, value := range c.TrustedProxyCIDRs {
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			problems = append(problems, fmt.Sprintf("trusted_proxy_cidrs شامل CIDR نامعتبر %q است", value))
			continue
		}
		prefix = prefix.Masked()
		if _, duplicate := trustedProxyPrefixes[prefix]; duplicate {
			problems = append(problems, fmt.Sprintf("trusted_proxy_cidrs شامل مقدار تکراری %q است", prefix.String()))
		}
		trustedProxyPrefixes[prefix] = struct{}{}
	}
	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		problems = append(problems, "log_level باید debug، info، warn یا error باشد")
	}
	if strings.TrimSpace(c.DataDir) == "" {
		problems = append(problems, "data_dir الزامی است")
	}
	if strings.TrimSpace(c.BackupDir) == "" || !filepath.IsAbs(c.BackupDir) {
		problems = append(problems, "backup_dir باید مسیر مطلق باشد")
	} else {
		dataDirectory := filepath.Clean(c.DataDir)
		backupDirectory := filepath.Clean(c.BackupDir)
		if backupDirectory == dataDirectory || strings.HasPrefix(backupDirectory+string(filepath.Separator), dataDirectory+string(filepath.Separator)) {
			problems = append(problems, "backup_dir باید خارج از data_dir باشد")
		}
	}
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

func applyString(target *string, key string) {
	if value, ok := os.LookupEnv(key); ok {
		*target = strings.TrimSpace(value)
	}
}

func splitCSV(value string) []string {
	items := make([]string, 0)
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			items = append(items, item)
		}
	}
	return items
}

func envInt32(key string, fallback int32) (int32, error) {
	value, ok := os.LookupEnv(key)
	if !ok {
		return fallback, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return int32(parsed), nil
}

func envInt64(key string, fallback int64) (int64, error) {
	value, ok := os.LookupEnv(key)
	if !ok {
		return fallback, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return parsed, nil
}
