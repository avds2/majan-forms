package publicform

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"net/mail"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"forms/internal/form"
	"forms/internal/id"
	"forms/internal/normalize"
	"forms/internal/persiandate"
)

var (
	ErrInvalidStart     = errors.New("صفحهٔ شروع منقضی شده است؛ صفحه را تازه کنید")
	ErrTooFast          = errors.New("فرم زودتر از زمان مجاز ارسال شد")
	ErrInvalidPage      = errors.New("صفحهٔ فرم معتبر نیست")
	ErrInvalidAnswers   = errors.New("پاسخ‌های این صفحه کامل یا معتبر نیستند")
	ErrInvalidSubmitKey = errors.New("کلید ثبت پاسخ معتبر نیست")
	ErrPathChanged      = errors.New("مسیر پاسخ تغییر کرده است؛ صفحهٔ جاری را دوباره بررسی کنید")
	phonePattern        = regexp.MustCompile(`^\+?[0-9][0-9 -]{6,19}$`)
)

type AnswerProblem struct {
	FieldID string `json:"field_id"`
	Message string `json:"message"`
}

type AnswerError struct{ Problems []AnswerProblem }

func (e *AnswerError) Error() string { return ErrInvalidAnswers.Error() }
func (e *AnswerError) Unwrap() error { return ErrInvalidAnswers }

type Navigation struct {
	Session Session
	Page    *form.Page
	Ending  *form.Ending
}

type Service struct {
	store          *Store
	signingKey     []byte
	location       *time.Location
	dataDir        string
	maxUploadBytes int64
	turnstileKey   string
	turnstile      TurnstileVerifier
	now            func() time.Time
}

func (s *Service) ConfigureTurnstile(siteKey string, verifier TurnstileVerifier) error {
	siteKey = strings.TrimSpace(siteKey)
	if siteKey == "" || verifier == nil {
		return errors.New("turnstile site key and verifier are required")
	}
	s.turnstileKey, s.turnstile = siteKey, verifier
	return nil
}

func (s *Service) TurnstileSiteKey() string { return s.turnstileKey }

func NewService(store *Store, signingKey []byte, location *time.Location, dataDir string, maxUploadBytes int64) (*Service, error) {
	if len(signingKey) < 32 {
		return nil, errors.New("public form signing key must contain at least 32 bytes")
	}
	if location == nil {
		return nil, errors.New("public form timezone is required")
	}
	if strings.TrimSpace(dataDir) == "" || maxUploadBytes < 1<<20 {
		return nil, errors.New("public form upload configuration is invalid")
	}
	return &Service{
		store: store, signingKey: append([]byte(nil), signingKey...), location: location,
		dataDir: dataDir, maxUploadBytes: maxUploadBytes, now: func() time.Time { return time.Now().UTC() },
	}, nil
}

func (s *Service) MaxRequestBytes() int64 { return s.maxUploadBytes*20 + (4 << 20) }

func (s *Service) analyticsDate(now time.Time) string {
	return now.In(s.location).Format(time.DateOnly)
}

func (s *Service) LoadLanding(ctx context.Context, slug string) (PublishedProject, string, error) {
	project, err := s.store.FindPublishedProject(ctx, normalize.Slug(slug))
	if err != nil {
		return PublishedProject{}, "", err
	}
	if err := project.Version.Schema.ValidateForPublish(); err != nil {
		return PublishedProject{}, "", fmt.Errorf("published form is invalid: %w", err)
	}
	if project.Status == "published" {
		if project.Version.Schema.PublicSettings.TurnstileEnabled && s.turnstile == nil {
			return PublishedProject{}, "", ErrTurnstileUnavailable
		}
		_ = s.store.incrementAnalytics(ctx, project.ProjectID, project.Version.ID, "", "views", s.analyticsDate(s.now()))
	}
	startToken, err := s.issueStartToken(project.Slug, s.now())
	if err != nil {
		return PublishedProject{}, "", err
	}
	return project, startToken, nil
}

func (s *Service) Start(ctx context.Context, slug, startToken, honeypot, turnstileToken, remoteIP string) (Session, string, error) {
	project, err := s.store.FindPublishedProject(ctx, normalize.Slug(slug))
	if err != nil {
		return Session{}, "", err
	}
	if project.Status != "published" {
		return Session{}, "", ErrUnavailable
	}
	if _, err := s.verifyStartToken(project.Slug, startToken); err != nil {
		return Session{}, "", err
	}
	if strings.TrimSpace(honeypot) != "" {
		return Session{}, "", ErrUnavailable
	}
	if project.Version.Schema.PublicSettings.TurnstileEnabled {
		if s.turnstile == nil || s.turnstileKey == "" {
			return Session{}, "", ErrTurnstileUnavailable
		}
		if strings.TrimSpace(turnstileToken) == "" {
			return Session{}, "", ErrTurnstileRequired
		}
		verified, verifyErr := s.turnstile.Verify(ctx, turnstileToken, remoteIP)
		if verifyErr != nil {
			return Session{}, "", ErrTurnstileUnavailable
		}
		if !verified {
			return Session{}, "", ErrTurnstileRequired
		}
	}
	now := s.now()
	token, err := id.Token(32)
	if err != nil {
		return Session{}, "", fmt.Errorf("create public token: %w", err)
	}
	hash := sha256.Sum256([]byte(token))
	session, err := s.store.CreateSession(ctx, project, hash[:], now, s.analyticsDate(now))
	return session, token, err
}

func (s *Service) Resume(ctx context.Context, slug, token string) (Navigation, error) {
	session, err := s.loadSession(ctx, slug, token)
	if err != nil {
		return Navigation{}, err
	}
	if ending := findEnding(session.Project.Version.Schema, session.CurrentPageID); ending != nil {
		return Navigation{Session: session, Ending: ending}, nil
	}
	page := findPage(session.Project.Version.Schema, session.CurrentPageID)
	if page == nil {
		return Navigation{}, ErrInvalidPage
	}
	return Navigation{Session: session, Page: page}, nil
}

func (s *Service) SubmitPage(ctx context.Context, slug, token, pageID string, values url.Values) (Navigation, error) {
	session, err := s.loadSession(ctx, slug, token)
	if err != nil {
		return Navigation{}, err
	}
	if session.CurrentPageID != pageID {
		return Navigation{}, ErrInvalidPage
	}
	page := findPage(session.Project.Version.Schema, pageID)
	if page == nil {
		return Navigation{}, ErrInvalidPage
	}
	parsed, problems := s.parsePageAnswers(*page, values)
	problems = append(problems, s.validateRequiredFiles(ctx, session, *page)...)
	if len(problems) > 0 {
		return Navigation{Session: session, Page: page}, &AnswerError{Problems: problems}
	}
	for _, field := range page.Fields {
		if !isComputedOrDisplay(field.Type) {
			delete(session.Answers, field.ID)
		}
	}
	for fieldID, value := range parsed {
		session.Answers[fieldID] = value
	}
	edge, err := form.NextEdge(session.Project.Version.Schema, page.ID, session.Answers)
	if err != nil {
		return Navigation{}, fmt.Errorf("select next public page: %w", err)
	}
	form.ApplyAssignments(session.Project.Version.Schema, edge.Actions, session.Answers)
	now := s.now()
	session.LastSeenAt, session.ExpiresAt = now, now.Add(publicSessionLifetime)
	if ending := findEnding(session.Project.Version.Schema, edge.TargetID); ending != nil {
		session.History = append(session.History, page.ID)
		session.CurrentPageID = ending.ID
		pruneAnswers(&session, session.History)
		if err := s.store.UpdateSession(ctx, &session, page.ID, s.analyticsDate(now)); err != nil {
			return Navigation{}, err
		}
		if err := s.pruneStagedFiles(ctx, session, session.History); err != nil {
			return Navigation{}, err
		}
		return Navigation{Session: session, Ending: ending}, nil
	}
	nextPage := findPage(session.Project.Version.Schema, edge.TargetID)
	if nextPage == nil {
		return Navigation{}, ErrInvalidPage
	}
	session.History = append(session.History, page.ID)
	session.CurrentPageID = nextPage.ID
	reachablePages := append(append([]string{}, session.History...), nextPage.ID)
	pruneAnswers(&session, reachablePages)
	if err := s.store.UpdateSession(ctx, &session, page.ID, s.analyticsDate(now)); err != nil {
		return Navigation{}, err
	}
	if err := s.pruneStagedFiles(ctx, session, reachablePages); err != nil {
		return Navigation{}, err
	}
	return Navigation{Session: session, Page: nextPage}, nil
}

func (s *Service) Back(ctx context.Context, slug, token string) (Navigation, error) {
	session, err := s.loadSession(ctx, slug, token)
	if err != nil {
		return Navigation{}, err
	}
	if len(session.History) == 0 {
		page := findPage(session.Project.Version.Schema, session.CurrentPageID)
		return Navigation{Session: session, Page: page}, nil
	}
	previous := session.History[len(session.History)-1]
	session.History = session.History[:len(session.History)-1]
	session.CurrentPageID = previous
	now := s.now()
	session.LastSeenAt, session.ExpiresAt = now, now.Add(publicSessionLifetime)
	if err := s.store.UpdateSession(ctx, &session, "", s.analyticsDate(now)); err != nil {
		return Navigation{}, err
	}
	page := findPage(session.Project.Version.Schema, previous)
	if page == nil {
		return Navigation{}, ErrInvalidPage
	}
	return Navigation{Session: session, Page: page}, nil
}

func (s *Service) Submit(ctx context.Context, slug, token, idempotencyKey, fingerprint string) (Submission, form.Ending, bool, error) {
	if len(strings.TrimSpace(idempotencyKey)) < 16 || len(idempotencyKey) > 200 {
		return Submission{}, form.Ending{}, false, ErrInvalidSubmitKey
	}
	session, err := s.loadSessionForSubmit(ctx, slug, token)
	if err != nil {
		return Submission{}, form.Ending{}, false, err
	}
	if session.SubmittedAt == nil && s.now().Sub(session.StartedAt) < time.Duration(session.Project.Version.Schema.PublicSettings.MinimumFillSeconds)*time.Second {
		return Submission{}, form.Ending{}, false, ErrTooFast
	}
	currentEnding := findEnding(session.Project.Version.Schema, session.CurrentPageID)
	if currentEnding == nil {
		return Submission{}, form.Ending{}, false, ErrPathChanged
	}
	result, err := form.Evaluate(session.Project.Version.Schema, session.Answers)
	if err != nil {
		return Submission{}, form.Ending{}, false, err
	}
	expectedPath := append([]string{}, session.History...)
	if !equalStrings(result.PageIDs, expectedPath) {
		return Submission{}, form.Ending{}, false, ErrPathChanged
	}
	for _, pageID := range result.PageIDs {
		page := findPage(session.Project.Version.Schema, pageID)
		if page == nil {
			return Submission{}, form.Ending{}, false, ErrInvalidPage
		}
		if problems := validateStoredPageAnswers(*page, result.Answers, s.location); len(problems) > 0 {
			return Submission{}, form.Ending{}, false, &AnswerError{Problems: problems}
		}
		if session.SubmittedAt == nil {
			if problems := s.validateRequiredFiles(ctx, session, *page); len(problems) > 0 {
				return Submission{}, form.Ending{}, false, &AnswerError{Problems: problems}
			}
		}
	}
	ending := findEnding(session.Project.Version.Schema, result.EndingID)
	if ending == nil || ending.ID != currentEnding.ID {
		return Submission{}, form.Ending{}, false, ErrPathChanged
	}
	idempotencyHash := sha256.Sum256([]byte(strings.TrimSpace(idempotencyKey)))
	var fingerprintHash []byte
	if fingerprint = strings.TrimSpace(fingerprint); fingerprint != "" {
		hash := sha256.Sum256([]byte(fingerprint))
		fingerprintHash = hash[:]
	}
	session.Answers = result.Answers
	now := s.now()
	submission, duplicate, err := s.store.CreateSubmission(ctx, session, ending.ID, idempotencyHash[:], fingerprintHash, now, s.analyticsDate(now))
	if err != nil {
		return Submission{}, form.Ending{}, false, err
	}
	submission.Theme = session.Project.Version.Schema.Theme.Preset
	submission.Alignment = session.Project.Version.Schema.Theme.Alignment
	if err := s.finalizeSubmissionFiles(ctx, submission.ID); err != nil {
		return Submission{}, form.Ending{}, duplicate, err
	}
	return submission, *ending, duplicate, nil
}

func (s *Service) loadSession(ctx context.Context, slug, token string) (Session, error) {
	if len(token) < 32 || len(token) > 200 {
		return Session{}, ErrInvalidSession
	}
	hash := sha256.Sum256([]byte(token))
	return s.store.GetSession(ctx, normalize.Slug(slug), hash[:], s.now())
}

func (s *Service) loadSessionForSubmit(ctx context.Context, slug, token string) (Session, error) {
	if len(token) < 32 || len(token) > 200 {
		return Session{}, ErrInvalidSession
	}
	hash := sha256.Sum256([]byte(token))
	return s.store.GetSessionForSubmit(ctx, normalize.Slug(slug), hash[:], s.now())
}

func (s *Service) issueStartToken(slug string, now time.Time) (string, error) {
	nonce, err := id.Token(16)
	if err != nil {
		return "", fmt.Errorf("create public start token: %w", err)
	}
	payload := fmt.Sprintf("%d|%s|%s", now.Unix(), slug, nonce)
	encoded := base64.RawURLEncoding.EncodeToString([]byte(payload))
	mac := hmac.New(sha256.New, s.signingKey)
	mac.Write([]byte(encoded))
	return encoded + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (s *Service) verifyStartToken(slug, token string) (time.Time, error) {
	encoded, signature, ok := strings.Cut(token, ".")
	if !ok {
		return time.Time{}, ErrInvalidStart
	}
	provided, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil {
		return time.Time{}, ErrInvalidStart
	}
	mac := hmac.New(sha256.New, s.signingKey)
	mac.Write([]byte(encoded))
	if !hmac.Equal(provided, mac.Sum(nil)) {
		return time.Time{}, ErrInvalidStart
	}
	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return time.Time{}, ErrInvalidStart
	}
	parts := strings.Split(string(decoded), "|")
	if len(parts) != 3 || parts[1] != slug {
		return time.Time{}, ErrInvalidStart
	}
	seconds, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return time.Time{}, ErrInvalidStart
	}
	issuedAt := time.Unix(seconds, 0).UTC()
	now := s.now()
	if issuedAt.After(now.Add(time.Minute)) || now.Sub(issuedAt) > 30*time.Minute {
		return time.Time{}, ErrInvalidStart
	}
	return issuedAt, nil
}

func (s *Service) parsePageAnswers(page form.Page, values url.Values) (map[string]any, []AnswerProblem) {
	answers := make(map[string]any)
	problems := make([]AnswerProblem, 0)
	for _, field := range page.Fields {
		value, present, message := s.parseField(field, values)
		if message != "" {
			problems = append(problems, AnswerProblem{FieldID: field.ID, Message: message})
			continue
		}
		if present {
			answers[field.ID] = value
		} else if field.Required && !isComputedOrDisplay(field.Type) && field.Type != form.FieldFile && field.Type != form.FieldSignature {
			problems = append(problems, AnswerProblem{FieldID: field.ID, Message: "پاسخ به «" + field.Label + "» الزامی است"})
		}
	}
	return answers, problems
}

func (s *Service) parseField(field form.Field, values url.Values) (any, bool, string) {
	if isComputedOrDisplay(field.Type) || field.Type == form.FieldFile || field.Type == form.FieldSignature {
		return nil, false, ""
	}
	if field.Type == form.FieldMultiChoice || field.Type == form.FieldMultiDropdown {
		items := values[field.ID]
		if len(items) == 0 {
			return nil, false, ""
		}
		allowed := optionSet(field.Options)
		seen := make(map[string]bool, len(items))
		answer := make([]any, 0, len(items))
		for _, item := range items {
			if !allowed[item] || seen[item] {
				return nil, false, "یکی از گزینه‌های انتخاب‌شده معتبر نیست"
			}
			seen[item] = true
			answer = append(answer, item)
		}
		return answer, true, ""
	}
	if field.Type == form.FieldFullName {
		first, last := strings.TrimSpace(values.Get(field.ID+"_first")), strings.TrimSpace(values.Get(field.ID+"_last"))
		if first == "" && last == "" {
			return nil, false, ""
		}
		if first == "" || last == "" {
			return nil, false, "نام و نام خانوادگی را کامل وارد کنید"
		}
		if utf8.RuneCountInString(first) > 300 || utf8.RuneCountInString(last) > 300 {
			return nil, false, "نام یا نام خانوادگی بیش از حد بلند است"
		}
		return map[string]any{"first": first, "last": last}, true, ""
	}
	if field.Type == form.FieldAddress {
		address := strings.TrimSpace(values.Get(field.ID + "_address"))
		if address == "" {
			return nil, false, ""
		}
		province := strings.TrimSpace(values.Get(field.ID + "_province"))
		city := strings.TrimSpace(values.Get(field.ID + "_city"))
		postalCode := normalize.Digits(strings.TrimSpace(values.Get(field.ID + "_postal")))
		if utf8.RuneCountInString(province) > 200 || utf8.RuneCountInString(city) > 200 || utf8.RuneCountInString(address) > 5000 || len(postalCode) > 20 {
			return nil, false, "نشانی واردشده بیش از حد بلند است"
		}
		return map[string]any{
			"province":    province,
			"city":        city,
			"address":     address,
			"postal_code": postalCode,
		}, true, ""
	}
	if field.Type == form.FieldDateRange {
		start, end := values.Get(field.ID+"_start"), values.Get(field.ID+"_end")
		if strings.TrimSpace(start) == "" && strings.TrimSpace(end) == "" {
			return nil, false, ""
		}
		startISO, startErr := persianToISO(start)
		endISO, endErr := persianToISO(end)
		if startErr != nil || endErr != nil || startISO > endISO {
			return nil, false, "بازهٔ تاریخ شمسی معتبر نیست"
		}
		if !dateWithinRule(field.Date, startISO) || !dateWithinRule(field.Date, endISO) {
			return nil, false, "بازهٔ تاریخ خارج از محدودهٔ مجاز است"
		}
		return map[string]any{"start": startISO, "end": endISO}, true, ""
	}
	if field.Type == form.FieldMatrix {
		matrix := make(map[string]any)
		if field.Matrix == nil {
			return nil, false, "ساختار ماتریس معتبر نیست"
		}
		columns := optionSet(field.Matrix.Columns)
		for _, row := range field.Matrix.Rows {
			selected := values[row.ID]
			if len(selected) == 0 {
				continue
			}
			if !field.Matrix.Multiple && len(selected) != 1 {
				return nil, false, "برای هر سطر ماتریس فقط یک گزینه مجاز است"
			}
			seen := make(map[string]bool, len(selected))
			for _, column := range selected {
				if !columns[column] || seen[column] {
					return nil, false, "پاسخ ماتریس معتبر نیست"
				}
				seen[column] = true
			}
			if field.Matrix.Multiple {
				matrix[row.ID] = selected
			} else {
				matrix[row.ID] = selected[0]
			}
		}
		if len(matrix) == 0 {
			return nil, false, ""
		}
		if field.Required && len(matrix) != len(field.Matrix.Rows) {
			return nil, false, "پاسخ همهٔ سطرهای ماتریس الزامی است"
		}
		return matrix, true, ""
	}
	raw := strings.TrimSpace(values.Get(field.ID))
	if raw == "" {
		return nil, false, ""
	}
	switch {
	case numericField(field.Type):
		number, err := strconv.ParseFloat(normalize.Digits(raw), 64)
		if err != nil || math.IsNaN(number) || math.IsInf(number, 0) {
			return nil, false, "عدد واردشده معتبر نیست"
		}
		if field.Number != nil && (field.Number.Minimum != nil && number < *field.Number.Minimum || field.Number.Maximum != nil && number > *field.Number.Maximum) {
			return nil, false, "عدد خارج از محدودهٔ مجاز است"
		}
		if field.Number != nil && field.Number.Integer && number != math.Trunc(number) {
			return nil, false, "عدد باید صحیح باشد"
		}
		if field.Type == form.FieldPercent && (number < 0 || number > 100) {
			return nil, false, "درصد باید بین صفر و صد باشد"
		}
		if field.Type == form.FieldRating || field.Type == form.FieldScale || field.Type == form.FieldNPS {
			minimum, maximum := 1, 5
			if field.Type == form.FieldNPS {
				minimum, maximum = 0, 10
			}
			if field.Scale != nil {
				minimum, maximum = field.Scale.Minimum, field.Scale.Maximum
			}
			if number < float64(minimum) || number > float64(maximum) || number != math.Trunc(number) {
				return nil, false, "امتیاز خارج از محدودهٔ مجاز است"
			}
		}
		return number, true, ""
	case field.Type == form.FieldYesNo:
		if raw != "true" && raw != "false" {
			return nil, false, "گزینهٔ بله یا خیر را انتخاب کنید"
		}
		return raw == "true", true, ""
	case field.Type == form.FieldConsent:
		if raw != "accepted" {
			return nil, false, "برای ادامه باید متن رضایت را بپذیرید"
		}
		return true, true, ""
	case field.Type == form.FieldSingleChoice || field.Type == form.FieldDropdown:
		if !optionSet(field.Options)[raw] {
			return nil, false, "گزینهٔ انتخاب‌شده معتبر نیست"
		}
		return raw, true, ""
	case field.Type == form.FieldPersianDate:
		iso, err := persianToISO(raw)
		if err != nil {
			return nil, false, "تاریخ شمسی معتبر نیست"
		}
		if !dateWithinRule(field.Date, iso) {
			return nil, false, "تاریخ خارج از محدودهٔ مجاز است"
		}
		return iso, true, ""
	case field.Type == form.FieldTime:
		normalized := normalize.Digits(raw)
		if _, err := time.Parse("15:04", normalized); err != nil {
			return nil, false, "زمان معتبر نیست"
		}
		return normalized, true, ""
	case field.Type == form.FieldDateTime:
		parts := strings.Fields(raw)
		if len(parts) != 2 {
			return nil, false, "تاریخ و ساعت معتبر نیست"
		}
		date, err := persiandate.Parse(parts[0])
		if err != nil {
			return nil, false, "تاریخ و ساعت معتبر نیست"
		}
		gregorian, _ := date.Gregorian()
		clock, err := time.Parse("15:04", normalize.Digits(parts[1]))
		if err != nil {
			return nil, false, "تاریخ و ساعت معتبر نیست"
		}
		local := time.Date(gregorian.Year(), gregorian.Month(), gregorian.Day(), clock.Hour(), clock.Minute(), 0, 0, s.location)
		if !dateWithinRule(field.Date, gregorian.Format(time.DateOnly)) {
			return nil, false, "تاریخ و ساعت خارج از محدودهٔ مجاز است"
		}
		return local.UTC().Format(time.RFC3339), true, ""
	default:
		if message := validateTextAnswer(field, raw); message != "" {
			return nil, false, message
		}
		return raw, true, ""
	}
}

func validateStoredPageAnswers(page form.Page, answers map[string]any, location *time.Location) []AnswerProblem {
	problems := make([]AnswerProblem, 0)
	for _, field := range page.Fields {
		if isComputedOrDisplay(field.Type) || field.Type == form.FieldFile || field.Type == form.FieldSignature {
			continue
		}
		value, ok := answers[field.ID]
		if !ok || emptyAnswer(value) {
			if !field.Required {
				continue
			}
			problems = append(problems, AnswerProblem{FieldID: field.ID, Message: "پاسخ به «" + field.Label + "» الزامی است"})
			continue
		}
		if !canonicalAnswerValidInLocation(field, value, location) {
			problems = append(problems, AnswerProblem{FieldID: field.ID, Message: "پاسخ ذخیره‌شدهٔ «" + field.Label + "» معتبر نیست"})
		}
	}
	return problems
}

func canonicalAnswerValid(field form.Field, value any) bool {
	return canonicalAnswerValidInLocation(field, value, time.UTC)
}

func canonicalAnswerValidInLocation(field form.Field, value any, location *time.Location) bool {
	switch field.Type {
	case form.FieldFullName:
		parts, ok := value.(map[string]any)
		if !ok {
			return false
		}
		first, firstOK := parts["first"].(string)
		last, lastOK := parts["last"].(string)
		return firstOK && lastOK && strings.TrimSpace(first) != "" && strings.TrimSpace(last) != "" && utf8.RuneCountInString(first) <= 300 && utf8.RuneCountInString(last) <= 300
	case form.FieldAddress:
		parts, ok := value.(map[string]any)
		if !ok {
			return false
		}
		address, addressOK := parts["address"].(string)
		province, provinceOK := parts["province"].(string)
		city, cityOK := parts["city"].(string)
		postal, postalOK := parts["postal_code"].(string)
		return addressOK && provinceOK && cityOK && postalOK && strings.TrimSpace(address) != "" &&
			utf8.RuneCountInString(address) <= 5000 && utf8.RuneCountInString(province) <= 200 && utf8.RuneCountInString(city) <= 200 && len(postal) <= 20
	case form.FieldDateRange:
		parts, ok := value.(map[string]any)
		if !ok {
			return false
		}
		start, startOK := parts["start"].(string)
		end, endOK := parts["end"].(string)
		if !startOK || !endOK || start > end || !dateWithinRule(field.Date, start) || !dateWithinRule(field.Date, end) {
			return false
		}
		_, startErr := time.Parse(time.DateOnly, start)
		_, endErr := time.Parse(time.DateOnly, end)
		return startErr == nil && endErr == nil
	case form.FieldMatrix:
		matrix, ok := value.(map[string]any)
		if !ok || field.Matrix == nil || len(matrix) == 0 || field.Required && len(matrix) != len(field.Matrix.Rows) {
			return false
		}
		rows, columns := optionSet(field.Matrix.Rows), optionSet(field.Matrix.Columns)
		for rowID, selected := range matrix {
			if !rows[rowID] {
				return false
			}
			if field.Matrix.Multiple {
				items, ok := selected.([]any)
				if !ok || len(items) == 0 {
					return false
				}
				seen := make(map[string]bool, len(items))
				for _, item := range items {
					text, ok := item.(string)
					if !ok || !columns[text] || seen[text] {
						return false
					}
					seen[text] = true
				}
			} else {
				text, ok := selected.(string)
				if !ok || !columns[text] {
					return false
				}
			}
		}
		return true
	case form.FieldMultiChoice, form.FieldMultiDropdown:
		items, ok := value.([]any)
		if !ok || len(items) == 0 {
			return false
		}
		allowed := optionSet(field.Options)
		seen := make(map[string]bool, len(items))
		for _, item := range items {
			text, ok := item.(string)
			if !ok || !allowed[text] || seen[text] {
				return false
			}
			seen[text] = true
		}
		return true
	case form.FieldSingleChoice, form.FieldDropdown:
		text, ok := value.(string)
		return ok && optionSet(field.Options)[text]
	case form.FieldYesNo:
		_, ok := value.(bool)
		return ok
	case form.FieldConsent:
		accepted, ok := value.(bool)
		return ok && accepted
	case form.FieldPersianDate:
		text, ok := value.(string)
		if !ok || !dateWithinRule(field.Date, text) {
			return false
		}
		_, err := time.Parse(time.DateOnly, text)
		return err == nil
	case form.FieldDateTime:
		text, ok := value.(string)
		if !ok {
			return false
		}
		instant, err := time.Parse(time.RFC3339, text)
		return err == nil && location != nil && dateWithinRule(field.Date, instant.In(location).Format(time.DateOnly))
	case form.FieldTime:
		text, ok := value.(string)
		if !ok {
			return false
		}
		_, err := time.Parse("15:04", text)
		return err == nil
	case form.FieldNumber, form.FieldMoney, form.FieldPercent, form.FieldRating, form.FieldScale, form.FieldNPS:
		number, ok := value.(float64)
		if !ok || math.IsNaN(number) || math.IsInf(number, 0) {
			return false
		}
		if field.Number != nil && (field.Number.Minimum != nil && number < *field.Number.Minimum || field.Number.Maximum != nil && number > *field.Number.Maximum || field.Number.Integer && number != math.Trunc(number)) {
			return false
		}
		if field.Type == form.FieldPercent && (number < 0 || number > 100) {
			return false
		}
		if field.Type == form.FieldRating || field.Type == form.FieldScale || field.Type == form.FieldNPS {
			minimum, maximum := 1, 5
			if field.Type == form.FieldNPS {
				minimum, maximum = 0, 10
			}
			if field.Scale != nil {
				minimum, maximum = field.Scale.Minimum, field.Scale.Maximum
			}
			return number == math.Trunc(number) && number >= float64(minimum) && number <= float64(maximum)
		}
		return true
	default:
		text, ok := value.(string)
		return ok && validateTextAnswer(field, text) == ""
	}
}

func validateTextAnswer(field form.Field, value string) string {
	length := utf8.RuneCountInString(value)
	if length > 100_000 {
		return "پاسخ متنی بیش از حد بلند است"
	}
	if field.Text != nil {
		if length < field.Text.MinimumLength || field.Text.MaximumLength > 0 && length > field.Text.MaximumLength {
			return "طول پاسخ در محدودهٔ مجاز نیست"
		}
		if field.Text.Pattern != "" {
			pattern, err := regexp.Compile(field.Text.Pattern)
			if err != nil || !pattern.MatchString(value) {
				return "قالب پاسخ معتبر نیست"
			}
		}
	}
	switch field.Type {
	case form.FieldEmail:
		address, err := mail.ParseAddress(value)
		if err != nil || address.Address != value || len(value) > 254 {
			return "ایمیل معتبر نیست"
		}
	case form.FieldPhone:
		if !phonePattern.MatchString(normalize.Digits(value)) {
			return "شمارهٔ تلفن معتبر نیست"
		}
	case form.FieldURL:
		parsed, err := url.ParseRequestURI(value)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
			return "نشانی باید HTTPS معتبر باشد"
		}
	case form.FieldNationalID:
		if !validIranianNationalID(normalize.Digits(value)) {
			return "کد ملی معتبر نیست"
		}
	}
	return ""
}

func dateWithinRule(rule *form.DateRule, value string) bool {
	if rule == nil {
		return true
	}
	return (rule.Minimum == "" || value >= rule.Minimum) && (rule.Maximum == "" || value <= rule.Maximum)
}

func persianToISO(value string) (string, error) {
	date, err := persiandate.Parse(value)
	if err != nil {
		return "", err
	}
	gregorian, err := date.Gregorian()
	if err != nil {
		return "", err
	}
	return gregorian.Format(time.DateOnly), nil
}

func validIranianNationalID(value string) bool {
	if len(value) != 10 {
		return false
	}
	allSame := true
	sum := 0
	for index := 0; index < 9; index++ {
		if value[index] < '0' || value[index] > '9' {
			return false
		}
		if index > 0 && value[index] != value[0] {
			allSame = false
		}
		sum += int(value[index]-'0') * (10 - index)
	}
	if value[9] < '0' || value[9] > '9' || allSame && value[9] == value[0] {
		return false
	}
	remainder := sum % 11
	if remainder >= 2 {
		remainder = 11 - remainder
	}
	return remainder == int(value[9]-'0')
}

func findPage(schema form.Schema, pageID string) *form.Page {
	for index := range schema.Pages {
		if schema.Pages[index].ID == pageID {
			return &schema.Pages[index]
		}
	}
	return nil
}

func findEnding(schema form.Schema, endingID string) *form.Ending {
	for index := range schema.Endings {
		if schema.Endings[index].ID == endingID {
			return &schema.Endings[index]
		}
	}
	return nil
}

func pruneAnswers(session *Session, reachablePages []string) {
	reachable := make(map[string]bool, len(reachablePages))
	for _, pageID := range reachablePages {
		reachable[pageID] = true
	}
	for _, page := range session.Project.Version.Schema.Pages {
		if reachable[page.ID] {
			continue
		}
		for _, field := range page.Fields {
			if field.Type != form.FieldHidden && field.Type != form.FieldCalculated {
				delete(session.Answers, field.ID)
			}
		}
	}
}

func optionSet(options []form.Option) map[string]bool {
	result := make(map[string]bool, len(options))
	for _, option := range options {
		result[option.ID] = true
	}
	return result
}

func isComputedOrDisplay(fieldType form.FieldType) bool {
	return fieldType == form.FieldHidden || fieldType == form.FieldCalculated || fieldType == form.FieldHeading || fieldType == form.FieldDescription || fieldType == form.FieldDivider || fieldType == form.FieldImage || fieldType == form.FieldSpacer
}

func numericField(fieldType form.FieldType) bool {
	return fieldType == form.FieldNumber || fieldType == form.FieldMoney || fieldType == form.FieldPercent || fieldType == form.FieldRating || fieldType == form.FieldScale || fieldType == form.FieldNPS
}

func emptyAnswer(value any) bool {
	switch typed := value.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(typed) == ""
	case []any:
		return len(typed) == 0
	case map[string]any:
		return len(typed) == 0
	default:
		return false
	}
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func sortedKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
