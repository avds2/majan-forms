package auth

import (
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"forms/internal/normalize"
)

type Role string

const (
	RoleAdmin    Role = "admin"
	RoleDesigner Role = "designer"
	RoleViewer   Role = "viewer"
)

var (
	ErrForbidden          = errors.New("اجازهٔ انجام این عملیات را ندارید")
	ErrInvalidCredentials = errors.New("نام کاربری یا رمز عبور نادرست است")
	ErrInvalidSession     = errors.New("نشست معتبر نیست")
	ErrNotFound           = errors.New("کاربر پیدا نشد")
	ErrDuplicateUser      = errors.New("ایمیل یا نام کاربری قبلاً استفاده شده است")
	ErrLastAdmin          = errors.New("آخرین مدیر فعال را نمی‌توان غیرفعال یا تنزل نقش داد")
	ErrSelfLockout        = errors.New("نمی‌توانید دسترسی مدیر فعلی را از همین نشست حذف کنید")
	ErrUsersExist         = errors.New("مدیر اولیه قبلاً ساخته شده است")
	ErrInvalidUser        = errors.New("اطلاعات کاربر معتبر نیست")
)

type User struct {
	ID          string     `json:"id"`
	Email       string     `json:"email"`
	Username    string     `json:"username"`
	DisplayName string     `json:"display_name"`
	Role        Role       `json:"role"`
	Active      bool       `json:"active"`
	LastLoginAt *time.Time `json:"last_login_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

func (r Role) Valid() bool {
	return r == RoleAdmin || r == RoleDesigner || r == RoleViewer
}

func (r Role) CanManageUsers() bool { return r == RoleAdmin }

func (r Role) CanEditProjects() bool { return r == RoleAdmin || r == RoleDesigner }

type CreateUserInput struct {
	Email       string `json:"email"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Password    string `json:"password"`
	Role        Role   `json:"role"`
}

type UpdateUserInput struct {
	DisplayName string `json:"display_name"`
	Role        Role   `json:"role"`
	Active      bool   `json:"active"`
}

var simpleEmailPattern = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)

func normalizeCreateInput(input CreateUserInput) (CreateUserInput, string, string, error) {
	input.Email = strings.TrimSpace(input.Email)
	input.Username = strings.TrimSpace(input.Username)
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	emailNormalized := normalize.LoginIdentifier(input.Email)
	usernameNormalized := normalize.LoginIdentifier(input.Username)
	if !simpleEmailPattern.MatchString(input.Email) || utf8.RuneCountInString(input.Email) > 254 {
		return CreateUserInput{}, "", "", ErrInvalidUser
	}
	if !validUsername(input.Username) || input.DisplayName == "" || utf8.RuneCountInString(input.DisplayName) > 100 || !input.Role.Valid() {
		return CreateUserInput{}, "", "", ErrInvalidUser
	}
	if err := ValidatePassword(input.Password); err != nil {
		return CreateUserInput{}, "", "", err
	}
	return input, emailNormalized, usernameNormalized, nil
}

func validUsername(value string) bool {
	count := utf8.RuneCountInString(value)
	if count < 3 || count > 64 {
		return false
	}
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' || r == '.' {
			continue
		}
		return false
	}
	return true
}
