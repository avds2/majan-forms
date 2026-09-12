package ai

import (
	"context"
	"net"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"forms/internal/auth"
	"forms/internal/response"
)

var modelPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,199}$`)
var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

type Service struct {
	store    *Store
	secrets  *SecretStore
	provider *HTTPProvider
	reports  *response.Service
	requests chan struct{}
}

func NewService(store *Store, dataDir string, reports *response.Service) *Service {
	return &Service{
		store: store, secrets: NewSecretStore(dataDir), provider: NewHTTPProvider(),
		reports:  reports,
		requests: make(chan struct{}, 2),
	}
}

func (s *Service) ProviderSettings(ctx context.Context, actor auth.User) (ProviderSettings, error) {
	if actor.Role != auth.RoleAdmin {
		return ProviderSettings{}, auth.ErrForbidden
	}
	settings, err := s.store.ProviderSettings(ctx)
	if err != nil {
		return ProviderSettings{}, err
	}
	settings.HasAPIKey = s.secrets.Exists()
	settings.Compatible = settings.CompatibilityTestedAt != nil
	return settings, nil
}

func (s *Service) SaveProviderSettings(ctx context.Context, actor auth.User, input SaveProviderInput) (ProviderSettings, error) {
	if actor.Role != auth.RoleAdmin {
		return ProviderSettings{}, auth.ErrForbidden
	}
	input.BaseURL = strings.TrimSpace(input.BaseURL)
	input.Model = strings.TrimSpace(input.Model)
	if err := validateProviderInput(ctx, input); err != nil {
		return ProviderSettings{}, err
	}
	saved, err := s.store.SaveProviderSettings(ctx, actor.ID, input)
	if err != nil {
		return ProviderSettings{}, err
	}
	if input.APIKey != "" {
		if err := s.secrets.Save(input.APIKey); err != nil {
			return ProviderSettings{}, err
		}
	}
	saved.HasAPIKey = s.secrets.Exists()
	saved.Compatible = false
	return saved, nil
}

func (s *Service) TestProvider(ctx context.Context, actor auth.User) (Compatibility, error) {
	if actor.Role != auth.RoleAdmin {
		return Compatibility{}, auth.ErrForbidden
	}
	settings, err := s.store.ProviderSettings(ctx)
	if err != nil {
		return Compatibility{}, err
	}
	// Compatibility is a proof from the latest successful test, not a sticky
	// promise. Clear it before re-testing so any offline/failed provider is
	// represented conservatively without changing its saved configuration.
	settings.UpdatedAt, err = s.store.BeginProviderCompatibilityTest(ctx, actor.ID, settings.UpdatedAt)
	if err != nil {
		return Compatibility{}, err
	}
	key, err := s.secrets.Read()
	if err != nil || settings.Model == "" {
		return Compatibility{}, ErrNotConfigured
	}
	select {
	case s.requests <- struct{}{}:
		defer func() { <-s.requests }()
	case <-ctx.Done():
		return Compatibility{}, ctx.Err()
	}
	result, err := s.provider.CompatibilityTest(ctx, providerConfig{
		BaseURL: settings.BaseURL, Mode: settings.Mode, Model: settings.Model,
		Timeout:         time.Duration(settings.TimeoutSeconds) * time.Second,
		MaxOutputTokens: settings.MaxOutputTokens, AllowPrivateEndpoint: settings.AllowPrivateEndpoint,
	}, key)
	if err != nil {
		return Compatibility{}, err
	}
	if err := s.store.MarkProviderCompatible(ctx, actor.ID, settings.UpdatedAt); err != nil {
		return Compatibility{}, err
	}
	return result, nil
}

func (s *Service) ProjectSettings(ctx context.Context, _ auth.User, projectID string) (ProjectSettings, error) {
	settings, err := s.store.ProjectSettings(ctx, projectID)
	if err != nil {
		return ProjectSettings{}, err
	}
	provider, err := s.store.ProviderSettings(ctx)
	if err != nil {
		return ProjectSettings{}, err
	}
	settings.ProviderConfigured = provider.Model != "" && s.secrets.Exists() && provider.CompatibilityTestedAt != nil
	return settings, nil
}

func (s *Service) SaveProjectSettings(ctx context.Context, actor auth.User, projectID string, input SaveProjectSettingsInput) (ProjectSettings, error) {
	if !actor.Role.CanEditProjects() {
		return ProjectSettings{}, auth.ErrForbidden
	}
	if !input.Enabled && input.TextSamplingEnabled {
		return ProjectSettings{}, ErrInvalidSettings
	}
	saved, err := s.store.SaveProjectSettings(ctx, actor.ID, projectID, input)
	if err != nil {
		return ProjectSettings{}, err
	}
	provider, err := s.store.ProviderSettings(ctx)
	if err != nil {
		return ProjectSettings{}, err
	}
	saved.ProviderConfigured = provider.Model != "" && s.secrets.Exists() && provider.CompatibilityTestedAt != nil
	return saved, nil
}

func (s *Service) CreateConversation(ctx context.Context, actor auth.User, projectID string, input CreateConversationInput) (Conversation, error) {
	title := strings.TrimSpace(input.Title)
	if title == "" {
		title = "گفت‌وگوی تازه"
	}
	if !uuidPattern.MatchString(projectID) || utf8.RuneCountInString(title) > 160 {
		return Conversation{}, ErrInvalidMessage
	}
	return s.store.CreateConversation(ctx, actor.ID, projectID, title)
}

func (s *Service) ListConversations(ctx context.Context, actor auth.User, projectID string) ([]Conversation, error) {
	if !uuidPattern.MatchString(projectID) {
		return nil, ErrConversationNotFound
	}
	return s.store.ListConversations(ctx, actor.ID, projectID)
}

func (s *Service) Conversation(ctx context.Context, actor auth.User, projectID, conversationID string) (Conversation, error) {
	if !uuidPattern.MatchString(projectID) || !uuidPattern.MatchString(conversationID) {
		return Conversation{}, ErrConversationNotFound
	}
	return s.store.Conversation(ctx, actor.ID, projectID, conversationID)
}

func (s *Service) Messages(ctx context.Context, actor auth.User, projectID, conversationID string) ([]Message, error) {
	if !uuidPattern.MatchString(projectID) || !uuidPattern.MatchString(conversationID) {
		return nil, ErrConversationNotFound
	}
	return s.store.ListMessages(ctx, actor.ID, projectID, conversationID, 100)
}

func (s *Service) DeleteConversation(ctx context.Context, actor auth.User, projectID, conversationID string) error {
	if !uuidPattern.MatchString(projectID) || !uuidPattern.MatchString(conversationID) {
		return ErrConversationNotFound
	}
	return s.store.DeleteConversation(ctx, actor.ID, projectID, conversationID)
}

func (s *Service) StreamMessage(ctx context.Context, actor auth.User, projectID, conversationID string, input SendMessageInput, onDelta func(string) error) (Message, error) {
	content := strings.TrimSpace(input.Content)
	if !uuidPattern.MatchString(projectID) || !uuidPattern.MatchString(conversationID) ||
		content == "" || utf8.RuneCountInString(content) > 4000 || onDelta == nil {
		return Message{}, ErrInvalidMessage
	}

	// Project consent is deliberately checked before loading a secret or opening
	// any provider connection. Turning the feature off is therefore a hard stop.
	settings, err := s.store.ProjectSettings(ctx, projectID)
	if err != nil {
		return Message{}, err
	}
	if !settings.Enabled {
		return Message{}, ErrProjectDisabled
	}
	if s.reports == nil {
		return Message{}, ErrNotConfigured
	}
	if _, err := s.store.Conversation(ctx, actor.ID, projectID, conversationID); err != nil {
		return Message{}, err
	}
	providerSettings, err := s.store.ProviderSettings(ctx)
	if err != nil {
		return Message{}, err
	}
	apiKey, err := s.secrets.Read()
	if err != nil || providerSettings.Model == "" || providerSettings.CompatibilityTestedAt == nil {
		return Message{}, ErrNotConfigured
	}
	config := providerConfig{
		BaseURL: providerSettings.BaseURL, Mode: providerSettings.Mode, Model: providerSettings.Model,
		Timeout: time.Duration(providerSettings.TimeoutSeconds) * time.Second, MaxOutputTokens: providerSettings.MaxOutputTokens,
		AllowPrivateEndpoint: providerSettings.AllowPrivateEndpoint,
	}

	select {
	case s.requests <- struct{}{}:
		defer func() { <-s.requests }()
	case <-ctx.Done():
		return Message{}, ctx.Err()
	}
	providerSession, err := s.provider.openSession(ctx, config)
	if err != nil {
		return Message{}, err
	}
	defer providerSession.Close()
	history, err := s.store.ListMessages(ctx, actor.ID, projectID, conversationID, 30)
	if err != nil {
		return Message{}, err
	}
	prompt := modelPrompt{Messages: make([]modelMessage, 0, len(history)), Exchanges: []toolExchange{}}
	reversedHistory := make([]modelMessage, 0, len(history))
	historyBytes := 0
	for index := len(history) - 1; index >= 0; index-- {
		message := history[index]
		contentForModel := message.Content
		if !settings.TextSamplingEnabled && message.Role == "assistant" {
			if sampled, _ := message.ContextSummary["sampled_text"].(bool); sampled {
				contentForModel = "[پاسخ قدیمیِ مبتنی بر نمونهٔ متن، به‌دلیل خاموش بودن دسترسی متنی از زمینه حذف شد.]"
			}
		}
		if historyBytes+len(contentForModel) > 256<<10 {
			break
		}
		historyBytes += len(contentForModel)
		reversedHistory = append(reversedHistory, modelMessage{Role: message.Role, Content: contentForModel})
	}
	for index := len(reversedHistory) - 1; index >= 0; index-- {
		prompt.Messages = append(prompt.Messages, reversedHistory[index])
	}
	prompt.Messages = append(prompt.Messages, modelMessage{Role: "user", Content: content})
	definitions := s.toolDefinitions(settings.TextSamplingEnabled)
	contextUsed := toolContext{Names: []string{}}
	totalCalls := 0
	callIDs := make(map[string]bool)
	for roundNumber := 0; roundNumber < 3; roundNumber++ {
		round, roundErr := providerSession.StreamRound(ctx, apiKey, prompt, definitions, onDelta)
		if roundErr != nil {
			return Message{}, roundErr
		}
		if len(round.ToolCalls) == 0 {
			if strings.TrimSpace(round.Text) == "" {
				return Message{}, ErrProviderIncompatible
			}
			return s.store.InsertExchange(ctx, actor.ID, projectID, conversationID, content, strings.TrimSpace(round.Text), map[string]any{
				"tools": contextUsed.Names, "sampled_text": contextUsed.SampledText,
			})
		}
		if len(round.ToolCalls) > 4 || totalCalls+len(round.ToolCalls) > 12 {
			return Message{}, ErrToolLimit
		}
		exchange := toolExchange{Calls: round.ToolCalls, Outputs: make([]string, 0, len(round.ToolCalls))}
		for _, call := range round.ToolCalls {
			if callIDs[call.ID] {
				return Message{}, ErrProviderIncompatible
			}
			callIDs[call.ID] = true
			output, sampled, toolErr := s.executeTool(ctx, actor, projectID, settings, call)
			if toolErr != nil {
				return Message{}, toolErr
			}
			exchange.Outputs = append(exchange.Outputs, output)
			contextUsed.Names = append(contextUsed.Names, call.Name)
			contextUsed.SampledText = contextUsed.SampledText || sampled
			totalCalls++
		}
		prompt.Exchanges = append(prompt.Exchanges, exchange)
	}
	return Message{}, ErrToolLimit
}

func validateProviderInput(ctx context.Context, input SaveProviderInput) error {
	if input.Mode != ModeResponses && input.Mode != ModeChatCompletions || !modelPattern.MatchString(input.Model) ||
		input.TimeoutSeconds < 5 || input.TimeoutSeconds > 180 || input.MaxOutputTokens < 128 || input.MaxOutputTokens > 32768 {
		return ErrInvalidSettings
	}
	if input.APIKey != "" {
		trimmed := strings.TrimSpace(input.APIKey)
		if len(trimmed) < 8 || len(trimmed) > maxSecretBytes || strings.ContainsAny(trimmed, "\r\n\x00") {
			return ErrInvalidSettings
		}
	}
	parsed, err := ValidateEndpoint(ctx, input.BaseURL, input.AllowPrivateEndpoint, net.DefaultResolver)
	if err != nil || parsePort(parsed) != nil || canonicalAddress(parsed) == "" {
		return ErrUnsafeEndpoint
	}
	return nil
}
