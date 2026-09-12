package backup

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

var postgresToolVersion = regexp.MustCompile(`\b([0-9]+)(?:\.[0-9]+)*\b`)

type PostgreSQLToolVersions struct {
	ServerMajor  int `json:"server_major"`
	PGDumpMajor  int `json:"pg_dump_major"`
	RestoreMajor int `json:"pg_restore_major"`
}

// CheckPostgreSQLToolCompatibility enforces same-major PostgreSQL utilities.
// A newer pg_dump can connect to an older server, but its archive may contain
// settings the old server cannot restore, so successful creation is not enough.
func CheckPostgreSQLToolCompatibility(ctx context.Context, pool *pgxpool.Pool, pgDumpPath, pgRestorePath string) (PostgreSQLToolVersions, error) {
	if pool == nil {
		return PostgreSQLToolVersions{}, fmt.Errorf("اتصال پایگاه‌داده برای بررسی ابزارهای PostgreSQL لازم است")
	}
	var serverVersionNumber string
	if err := pool.QueryRow(ctx, `SHOW server_version_num`).Scan(&serverVersionNumber); err != nil {
		return PostgreSQLToolVersions{}, fmt.Errorf("read PostgreSQL server version: %w", err)
	}
	versionNumber, err := strconv.Atoi(serverVersionNumber)
	if err != nil || versionNumber < 100000 {
		return PostgreSQLToolVersions{}, fmt.Errorf("نسخهٔ سرور PostgreSQL قابل تشخیص نیست: %q", serverVersionNumber)
	}
	versions := PostgreSQLToolVersions{ServerMajor: versionNumber / 10000}
	versions.PGDumpMajor, err = postgresCommandMajor(ctx, pgDumpPath)
	if err != nil {
		return versions, fmt.Errorf("pg_dump: %w", err)
	}
	versions.RestoreMajor, err = postgresCommandMajor(ctx, pgRestorePath)
	if err != nil {
		return versions, fmt.Errorf("pg_restore: %w", err)
	}
	if versions.PGDumpMajor != versions.ServerMajor || versions.RestoreMajor != versions.ServerMajor {
		return versions, fmt.Errorf(
			"نسخهٔ اصلی ابزارهای PostgreSQL باید با سرور یکسان باشد (server=%d, pg_dump=%d, pg_restore=%d)",
			versions.ServerMajor, versions.PGDumpMajor, versions.RestoreMajor,
		)
	}
	return versions, nil
}

func postgresCommandMajor(ctx context.Context, commandPath string) (int, error) {
	command := exec.CommandContext(ctx, commandPath, "--version")
	var output limitedBuffer
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Run(); err != nil {
		return 0, fmt.Errorf("اجرای --version ناموفق بود: %w: %s", err, strings.TrimSpace(output.String()))
	}
	match := postgresToolVersion.FindStringSubmatch(output.String())
	if len(match) != 2 {
		return 0, fmt.Errorf("نسخه از خروجی قابل تشخیص نیست: %q", strings.TrimSpace(output.String()))
	}
	major, err := strconv.Atoi(match[1])
	if err != nil {
		return 0, fmt.Errorf("نسخه معتبر نیست: %w", err)
	}
	return major, nil
}
