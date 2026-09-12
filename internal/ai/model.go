// Package ai implements the opt-in, read-only data assistant and its provider
// boundary. Provider secrets are never stored in PostgreSQL or returned by API.
package ai

import (
	"errors"
	"time"
)

type APIMode string

const (
	ModeResponses       APIMode = "responses"
	ModeChatCompletions APIMode = "chat_completions"
)

var (
	ErrInvalidSettings       = errors.New("تنظیمات اتصال هوش مصنوعی معتبر نیست")
	ErrNotConfigured         = errors.New("اتصال هوش مصنوعی هنوز کامل پیکربندی نشده است")
	ErrProjectDisabled       = errors.New("گفت‌وگوی داده برای این پروژه خاموش است")
	ErrUnsafeEndpoint        = errors.New("نشانی provider از نظر امنیت شبکه مجاز نیست")
	ErrProviderUnauthorized  = errors.New("provider کلید API را نپذیرفت")
	ErrProviderRateLimited   = errors.New("provider موقتاً محدودیت درخواست دارد")
	ErrProviderUnavailable   = errors.New("provider هوش مصنوعی در دسترس نیست")
	ErrProviderTimeout       = errors.New("مهلت پاسخ provider به پایان رسید")
	ErrProviderIncompatible  = errors.New("provider با streaming و ابزارهای لازم سازگار نیست")
	ErrProviderToolArguments = errors.New("provider آرگومان معتبر برای ابزار تحلیل برنگرداند")
	ErrConversationNotFound  = errors.New("گفت‌وگو پیدا نشد")
	ErrConversationLimit     = errors.New("تعداد گفت‌وگوهای این پروژه به سقف رسیده است")
	ErrInvalidMessage        = errors.New("پیام گفت‌وگو معتبر نیست")
	ErrToolLimit             = errors.New("تعداد فراخوانی ابزار از سقف مجاز بیشتر شد")
)

type ProviderSettings struct {
	BaseURL               string     `json:"base_url"`
	Mode                  APIMode    `json:"api_mode"`
	Model                 string     `json:"model"`
	TimeoutSeconds        int        `json:"timeout_seconds"`
	MaxOutputTokens       int        `json:"max_output_tokens"`
	AllowPrivateEndpoint  bool       `json:"allow_private_endpoint"`
	HasAPIKey             bool       `json:"has_api_key"`
	Compatible            bool       `json:"compatible"`
	CompatibilityTestedAt *time.Time `json:"compatibility_tested_at,omitempty"`
	UpdatedAt             time.Time  `json:"updated_at"`
}

type SaveProviderInput struct {
	BaseURL              string  `json:"base_url"`
	Mode                 APIMode `json:"api_mode"`
	Model                string  `json:"model"`
	TimeoutSeconds       int     `json:"timeout_seconds"`
	MaxOutputTokens      int     `json:"max_output_tokens"`
	AllowPrivateEndpoint bool    `json:"allow_private_endpoint"`
	APIKey               string  `json:"api_key,omitempty"`
}

type ProjectSettings struct {
	ProjectID           string `json:"project_id"`
	Enabled             bool   `json:"enabled"`
	TextSamplingEnabled bool   `json:"text_sampling_enabled"`
	ProviderConfigured  bool   `json:"provider_configured"`
}

type SaveProjectSettingsInput struct {
	Enabled             bool `json:"enabled"`
	TextSamplingEnabled bool `json:"text_sampling_enabled"`
}

type Compatibility struct {
	Mode      APIMode `json:"api_mode"`
	Model     string  `json:"model"`
	Streaming bool    `json:"streaming"`
	Tools     bool    `json:"tools"`
	LatencyMS int64   `json:"latency_ms"`
}

type providerConfig struct {
	BaseURL              string
	Mode                 APIMode
	Model                string
	Timeout              time.Duration
	MaxOutputTokens      int
	AllowPrivateEndpoint bool
}

type Conversation struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"project_id"`
	CreatedBy string    `json:"created_by"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Message struct {
	ID             string         `json:"id"`
	ConversationID string         `json:"conversation_id"`
	Role           string         `json:"role"`
	Content        string         `json:"content"`
	ContextSummary map[string]any `json:"context_summary,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
}

type CreateConversationInput struct {
	Title string `json:"title"`
}

type SendMessageInput struct {
	Content string `json:"content"`
}

type modelMessage struct {
	Role    string
	Content string
}

type toolCall struct {
	ID        string
	Name      string
	Arguments string
}

type toolExchange struct {
	Calls   []toolCall
	Outputs []string
}

type modelPrompt struct {
	Messages  []modelMessage
	Exchanges []toolExchange
}

type streamRound struct {
	Text      string
	Deltas    []string
	ToolCalls []toolCall
}

type toolDefinition struct {
	Name        string
	Description string
	Parameters  map[string]any
}
