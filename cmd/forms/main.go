package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"

	"golang.org/x/term"

	"forms/internal/app"
	"forms/internal/auth"
	formsbackup "forms/internal/backup"
	"forms/internal/config"
	"forms/internal/logging"
	formsrestore "forms/internal/restore"
	"forms/internal/version"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "خطا:", err)
		code := 1
		var exitCoder interface{ ExitCode() int }
		if errors.As(err, &exitCoder) {
			code = exitCoder.ExitCode()
		}
		os.Exit(code)
	}
}

type exitError struct {
	err  error
	code int
}

func (e exitError) Error() string { return e.err.Error() }
func (e exitError) Unwrap() error { return e.err }
func (e exitError) ExitCode() int { return e.code }

func run(arguments []string) error {
	if len(arguments) == 0 {
		return usageError()
	}
	if help, ok := commandHelp(arguments); ok {
		fmt.Println(help)
		return nil
	}
	if arguments[0] == "version" {
		encoded, err := json.Marshal(version.Current())
		if err != nil {
			return err
		}
		fmt.Println(string(encoded))
		return nil
	}
	if !knownCommand(arguments[0]) {
		return usageError()
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("پیکربندی معتبر نیست: %w", err)
	}
	logger := logging.New(cfg.LogLevel)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	switch arguments[0] {
	case "serve":
		return app.Serve(ctx, cfg, logger)
	case "migrate":
		return runMigrate(ctx, cfg, logger, arguments[1:])
	case "admin":
		return runAdmin(ctx, cfg, logger, arguments[1:])
	case "cleanup":
		return runCleanup(ctx, cfg, logger, arguments[1:])
	case "backup":
		return runBackup(ctx, cfg, arguments[1:])
	case "doctor":
		return runDoctor(ctx, cfg, arguments[1:])
	case "restore":
		return runRestore(ctx, cfg, arguments[1:])
	default:
		return usageError()
	}
}

const rootUsage = "کاربرد: forms serve | migrate up|status | admin create|reset-password | backup create|verify|list|prune | restore verify|prepare|apply|pending | cleanup | doctor | version"

var commandUsages = map[string]string{
	"serve":   "کاربرد: forms serve\nاجرای سرویس وب؛ پیکربندی از environment یا FORMS_CONFIG_FILE خوانده می‌شود.",
	"migrate": "کاربرد: forms migrate up|status\nاعمال migrationهای معوق یا نمایش وضعیت schema.",
	"admin":   "کاربرد: forms admin create|reset-password\nساخت مدیر نخست یا بازنشانی امن رمز و ابطال نشست‌های قبلی.",
	"backup":  "کاربرد: forms backup create|verify|list|prune\nساخت، اعتبارسنجی، فهرست‌کردن و اعمال retention روی بکاپ‌ها.",
	"restore": "کاربرد: forms restore verify|prepare|apply|pending\nاعتبارسنجی و اجرای بازیابی آفلاین یا مدیریت handoff به systemd.",
	"cleanup": "کاربرد: forms cleanup\nپاک‌سازی نشست‌ها و فایل‌های موقت منقضی.",
	"doctor":  "کاربرد: forms doctor\nبررسی آمادگی مسیرها، PostgreSQL، migrationها و ابزارهای بکاپ.",
	"version": "کاربرد: forms version\nنمایش نسخه، commit و زمان ساخت به‌صورت JSON.",
}

func commandHelp(arguments []string) (string, bool) {
	if len(arguments) == 1 && isHelpArgument(arguments[0]) {
		return rootUsage, true
	}
	if len(arguments) == 2 && arguments[0] == "help" {
		help, ok := commandUsages[arguments[1]]
		return help, ok
	}
	if len(arguments) >= 2 && isHelpArgument(arguments[len(arguments)-1]) {
		help, ok := commandUsages[arguments[0]]
		return help, ok
	}
	return "", false
}

func isHelpArgument(argument string) bool {
	return argument == "help" || argument == "--help" || argument == "-h"
}

func knownCommand(command string) bool {
	_, ok := commandUsages[command]
	return ok
}

func runRestore(ctx context.Context, cfg config.Config, arguments []string) error {
	if len(arguments) == 0 {
		return errors.New("کاربرد: forms restore verify|prepare|apply|pending")
	}
	if err := app.PrepareDataDirectories(cfg.DataDir); err != nil {
		return err
	}
	manager, err := formsrestore.New(formsrestore.Options{
		DatabaseURL: cfg.DatabaseURL, DataDir: cfg.DataDir, BackupDir: cfg.BackupDir,
		DBMinConns: cfg.DBMinConns, DBMaxConns: cfg.DBMaxConns,
		PGDumpPath: "pg_dump", PGRestorePath: "pg_restore",
	})
	if err != nil {
		return err
	}
	switch arguments[0] {
	case "verify":
		if len(arguments) != 2 {
			return errors.New("کاربرد: forms restore verify /path/file.tar.gz")
		}
		summary, err := manager.Verify(ctx, arguments[1])
		if err != nil {
			return err
		}
		return printJSON(summary)
	case "prepare":
		if len(arguments) != 2 {
			return errors.New("کاربرد: forms restore prepare /path/file.tar.gz")
		}
		requestPath, err := manager.Prepare(ctx, arguments[1])
		if err != nil {
			return err
		}
		return printJSON(map[string]any{"pending": true, "request_path": requestPath})
	case "apply":
		flags := flag.NewFlagSet("restore apply", flag.ContinueOnError)
		confirm := flags.Bool("confirm", false, "confirm destructive offline restore")
		if err := flags.Parse(arguments[1:]); err != nil {
			return err
		}
		if !*confirm || flags.NArg() != 1 {
			return errors.New("کاربرد: forms restore apply --confirm /path/file.tar.gz")
		}
		result, err := manager.Apply(ctx, flags.Arg(0))
		if err != nil {
			return err
		}
		return printJSON(result)
	case "pending":
		if len(arguments) != 1 {
			return errors.New("کاربرد: forms restore pending")
		}
		result, err := manager.Pending(ctx)
		if err != nil {
			// EX_TEMPFAIL is listed in RestartPreventExitStatus so a broken
			// request cannot trap systemd in an automatic restore loop.
			return exitError{err: err, code: 75}
		}
		return printJSON(result)
	default:
		return errors.New("کاربرد: forms restore verify|prepare|apply|pending")
	}
}

type doctorCheck struct {
	Name    string `json:"name"`
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

func runDoctor(ctx context.Context, cfg config.Config, arguments []string) error {
	if len(arguments) != 0 {
		return errors.New("کاربرد: forms doctor")
	}
	checks := make([]doctorCheck, 0, 7)
	failed := false
	add := func(name string, err error, success string) {
		check := doctorCheck{Name: name, OK: err == nil, Message: success}
		if err != nil {
			check.Message = err.Error()
			failed = true
		}
		checks = append(checks, check)
	}
	add("data_directory", writableDirectory(cfg.DataDir), cfg.DataDir)
	add("backup_directory", writableDirectory(cfg.BackupDir), cfg.BackupDir)
	_, pgDumpErr := exec.LookPath("pg_dump")
	add("pg_dump", pgDumpErr, "available")
	_, pgRestoreErr := exec.LookPath("pg_restore")
	add("pg_restore", pgRestoreErr, "available")
	runtime, runtimeErr := app.OpenRuntime(ctx, cfg)
	add("database", runtimeErr, "connected")
	if runtimeErr == nil {
		status, migrationErr := runtime.Migrator.Status(ctx)
		if migrationErr == nil && status.Pending != 0 {
			migrationErr = fmt.Errorf("%d migration معوق است", status.Pending)
		}
		add("migrations", migrationErr, fmt.Sprintf("current=%d latest=%d", status.Current, status.Latest))
		toolVersions, toolErr := formsbackup.CheckPostgreSQLToolCompatibility(ctx, runtime.Pool, "pg_dump", "pg_restore")
		add("postgresql_tools", toolErr, fmt.Sprintf("server=%d pg_dump=%d pg_restore=%d", toolVersions.ServerMajor, toolVersions.PGDumpMajor, toolVersions.RestoreMajor))
		runtime.Close()
	}
	if err := printJSON(map[string]any{"ok": !failed, "checks": checks}); err != nil {
		return err
	}
	if failed {
		return errors.New("یک یا چند بررسی doctor ناموفق بود")
	}
	return nil
}

func writableDirectory(directory string) error {
	stat, err := os.Stat(directory)
	if err != nil {
		return err
	}
	if !stat.IsDir() {
		return errors.New("مسیر پوشه نیست")
	}
	if stat.Mode().Perm()&0o077 != 0 {
		return errors.New("دسترسی پوشه باید 0700 یا محدودتر باشد")
	}
	probe, err := os.CreateTemp(directory, ".doctor-*")
	if err != nil {
		return err
	}
	name := probe.Name()
	closeErr := probe.Close()
	removeErr := os.Remove(name)
	if closeErr != nil {
		return closeErr
	}
	return removeErr
}

func runBackup(ctx context.Context, cfg config.Config, arguments []string) error {
	if len(arguments) == 0 {
		return errors.New("کاربرد: forms backup create|verify|list|prune")
	}
	baseOptions := formsbackup.Options{BackupDir: cfg.BackupDir, PGRestorePath: "pg_restore"}
	switch arguments[0] {
	case "create":
		flags := flag.NewFlagSet("backup create", flag.ContinueOnError)
		output := flags.String("output", "", "absolute .tar.gz output path")
		if err := flags.Parse(arguments[1:]); err != nil || flags.NArg() != 0 {
			if err != nil {
				return err
			}
			return errors.New("کاربرد: forms backup create [--output /path/file.tar.gz]")
		}
		if err := app.PrepareDataDirectories(cfg.DataDir); err != nil {
			return err
		}
		runtime, err := app.OpenRuntime(ctx, cfg)
		if err != nil {
			return err
		}
		defer runtime.Close()
		if err := runtime.Migrator.CheckCurrent(ctx); err != nil {
			return fmt.Errorf("ابتدا migration را اجرا کنید: %w", err)
		}
		baseOptions.Pool, baseOptions.Migrator = runtime.Pool, runtime.Migrator
		baseOptions.DatabaseURL, baseOptions.DataDir, baseOptions.PGDumpPath = cfg.DatabaseURL, cfg.DataDir, "pg_dump"
		manager, err := formsbackup.New(baseOptions)
		if err != nil {
			return err
		}
		summary, err := manager.Create(ctx, *output, nonSecretSettings(cfg))
		if err != nil {
			return err
		}
		return printJSON(summary)
	case "verify":
		if len(arguments) != 2 {
			return errors.New("کاربرد: forms backup verify /path/file.tar.gz")
		}
		manager, err := formsbackup.New(baseOptions)
		if err != nil {
			return err
		}
		summary, err := manager.Verify(ctx, arguments[1])
		if err != nil {
			return err
		}
		return printJSON(summary)
	case "list":
		if len(arguments) != 1 {
			return errors.New("کاربرد: forms backup list")
		}
		manager, err := formsbackup.New(baseOptions)
		if err != nil {
			return err
		}
		backups, err := manager.List(ctx)
		if err != nil {
			return err
		}
		return printJSON(backups)
	case "prune":
		flags := flag.NewFlagSet("backup prune", flag.ContinueOnError)
		keep := flags.Int("keep", 14, "number of newest valid backups to retain")
		if err := flags.Parse(arguments[1:]); err != nil || flags.NArg() != 0 {
			if err != nil {
				return err
			}
			return errors.New("کاربرد: forms backup prune [--keep 14]")
		}
		manager, err := formsbackup.New(baseOptions)
		if err != nil {
			return err
		}
		removed, err := manager.Prune(ctx, *keep)
		if err != nil {
			return err
		}
		return printJSON(map[string]any{"removed": removed, "count": len(removed)})
	default:
		return errors.New("کاربرد: forms backup create|verify|list|prune")
	}
}

func nonSecretSettings(cfg config.Config) map[string]any {
	return map[string]any{
		"timezone": cfg.Timezone, "max_upload_bytes": cfg.MaxUploadBytes,
		"session_idle": cfg.SessionIdleText, "session_absolute": cfg.SessionAbsText,
		"db_max_conns": cfg.DBMaxConns, "db_min_conns": cfg.DBMinConns,
	}
}

func printJSON(value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	fmt.Println(string(encoded))
	return nil
}

func runCleanup(ctx context.Context, cfg config.Config, logger *slog.Logger, arguments []string) error {
	if len(arguments) != 0 {
		return errors.New("کاربرد: forms cleanup")
	}
	if err := app.PrepareDataDirectories(cfg.DataDir); err != nil {
		return err
	}
	runtime, err := app.OpenRuntime(ctx, cfg)
	if err != nil {
		return err
	}
	defer runtime.Close()
	if err := runtime.Migrator.CheckCurrent(ctx); err != nil {
		return fmt.Errorf("ابتدا migration را اجرا کنید: %w", err)
	}
	report, err := runtime.PublicForms.CleanupTemporary(ctx)
	if err != nil {
		return err
	}
	logger.Info("public temporary data cleanup completed",
		slog.Int("recovered_submissions", report.RecoveredSubmissions),
		slog.Int("expired_sessions", report.ExpiredSessions),
		slog.Int("removed_staged_files", report.RemovedStagedFiles),
		slog.Int("removed_orphan_files", report.RemovedOrphanFiles),
	)
	encoded, _ := json.Marshal(report)
	fmt.Println(string(encoded))
	return nil
}

func runMigrate(ctx context.Context, cfg config.Config, logger *slog.Logger, arguments []string) error {
	if len(arguments) != 1 {
		return errors.New("کاربرد: forms migrate up|status")
	}
	runtime, err := app.OpenRuntime(ctx, cfg)
	if err != nil {
		return err
	}
	defer runtime.Close()
	switch arguments[0] {
	case "up":
		count, err := runtime.Migrator.Up(ctx)
		if err != nil {
			return err
		}
		logger.Info("database migrations completed", slog.Int("applied", count))
		fmt.Printf("%d migration اجرا شد.\n", count)
		return nil
	case "status":
		status, err := runtime.Migrator.Status(ctx)
		if err != nil {
			return err
		}
		encoded, _ := json.Marshal(status)
		fmt.Println(string(encoded))
		return nil
	default:
		return errors.New("کاربرد: forms migrate up|status")
	}
}

func runAdmin(ctx context.Context, cfg config.Config, _ *slog.Logger, arguments []string) error {
	if len(arguments) == 0 {
		return errors.New("کاربرد: forms admin create|reset-password")
	}
	runtime, err := app.OpenRuntime(ctx, cfg)
	if err != nil {
		return err
	}
	defer runtime.Close()
	if err := runtime.Migrator.CheckCurrent(ctx); err != nil {
		return fmt.Errorf("ابتدا migration را اجرا کنید: %w", err)
	}
	switch arguments[0] {
	case "create":
		return createInitialAdmin(ctx, runtime.Auth, arguments[1:])
	case "reset-password":
		return resetAdminPassword(ctx, runtime.Auth, arguments[1:])
	default:
		return errors.New("کاربرد: forms admin create|reset-password")
	}
}

func createInitialAdmin(ctx context.Context, service *auth.Service, arguments []string) error {
	flags := flag.NewFlagSet("admin create", flag.ContinueOnError)
	email := flags.String("email", "", "admin email")
	username := flags.String("username", "", "admin username")
	name := flags.String("name", "", "display name")
	passwordFile := flags.String("password-file", "", "path to a 0600 password file")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if *email == "" || *username == "" || *name == "" {
		return errors.New("گزینه‌های --email، --username و --name الزامی‌اند")
	}
	password, err := readPassword(*passwordFile, true)
	if err != nil {
		return err
	}
	user, err := service.CreateInitialAdmin(ctx, auth.CreateUserInput{
		Email: *email, Username: *username, DisplayName: *name,
		Password: password, Role: auth.RoleAdmin,
	})
	if err != nil {
		return err
	}
	fmt.Printf("مدیر %s با شناسهٔ %s ساخته شد.\n", user.DisplayName, user.ID)
	return nil
}

func resetAdminPassword(ctx context.Context, service *auth.Service, arguments []string) error {
	flags := flag.NewFlagSet("admin reset-password", flag.ContinueOnError)
	identifier := flags.String("identifier", "", "email or username")
	passwordFile := flags.String("password-file", "", "path to a 0600 password file")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if *identifier == "" {
		return errors.New("گزینهٔ --identifier الزامی است")
	}
	password, err := readPassword(*passwordFile, true)
	if err != nil {
		return err
	}
	if err := service.ResetPasswordCLI(ctx, *identifier, password); err != nil {
		return err
	}
	fmt.Println("رمز عبور تغییر کرد و نشست‌های قبلی باطل شدند.")
	return nil
}

func readPassword(path string, confirm bool) (string, error) {
	if path != "" {
		file, err := os.Open(path)
		if err != nil {
			return "", fmt.Errorf("خواندن فایل رمز: %w", err)
		}
		defer file.Close()
		stat, err := file.Stat()
		if err != nil {
			return "", err
		}
		if stat.Mode().Perm()&0o077 != 0 {
			return "", errors.New("دسترسی فایل رمز باید 0600 یا محدودتر باشد")
		}
		contents, err := io.ReadAll(io.LimitReader(file, 1024))
		if err != nil {
			return "", err
		}
		return strings.TrimRight(string(contents), "\r\n"), nil
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", errors.New("برای ورودی غیرتعاملی از --password-file استفاده کنید")
	}
	reader := bufio.NewWriter(os.Stderr)
	fmt.Fprint(reader, "رمز عبور: ")
	reader.Flush()
	first, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	if !confirm {
		return string(first), nil
	}
	fmt.Fprint(os.Stderr, "تکرار رمز عبور: ")
	second, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	if string(first) != string(second) {
		return "", errors.New("دو رمز واردشده یکسان نیستند")
	}
	return string(first), nil
}

func usageError() error {
	return errors.New(rootUsage)
}
