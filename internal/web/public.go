package web

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"forms/internal/form"
	"forms/internal/id"
	"forms/internal/persiandate"
	"forms/internal/publicform"
)

//go:embed public_assets/*
var publicAssets embed.FS

var publicAssetVersions = func() map[string]string {
	versions := make(map[string]string, 2)
	for _, name := range []string{"forms.css", "forms.js"} {
		content, err := publicAssets.ReadFile("public_assets/" + name)
		if err != nil {
			panic(err)
		}
		digest := sha256.Sum256(content)
		versions[name] = hex.EncodeToString(digest[:6])
	}
	return versions
}()

var publicTemplate = template.Must(template.New("public").Funcs(template.FuncMap{
	"assetVersion": func(name string) string { return publicAssetVersions[name] },
	"join":         func(left, right string) string { return left + right },
	"joinList":     strings.Join,
	"persian":      persianNumber,
	"scaleValues": func(field form.Field) []int {
		minimum, maximum := 1, 5
		if field.Type == form.FieldNPS {
			minimum, maximum = 0, 10
		}
		if field.Scale != nil {
			minimum, maximum = field.Scale.Minimum, field.Scale.Maximum
		}
		values := make([]int, 0, maximum-minimum+1)
		for value := minimum; value <= maximum; value++ {
			values = append(values, value)
		}
		return values
	},
	"selected": func(selections map[string]map[string]bool, fieldID, value string) bool {
		return selections[fieldID][value]
	},
	"describedBy": func(field form.Field, errorMessage string) string {
		ids := make([]string, 0, 3)
		if strings.TrimSpace(field.HelpText) != "" {
			ids = append(ids, "help-"+field.ID)
		}
		if field.Type == form.FieldPersianDate || field.Type == form.FieldFile {
			ids = append(ids, "note-"+field.ID)
		}
		if strings.TrimSpace(errorMessage) != "" {
			ids = append(ids, "error-"+field.ID)
		}
		return strings.Join(ids, " ")
	},
}).ParseFS(publicAssets, "public_assets/public.html"))

type publicView struct {
	Stage            string
	Title            string
	Slug             string
	Theme            string
	Alignment        string
	VersionLabel     string
	Closed           bool
	StartToken       string
	SessionToken     string
	SubmitKey        string
	Page             *form.Page
	Fields           []form.Field
	Ending           *form.Ending
	CurrentStep      int
	TotalSteps       int
	ProgressPercent  int
	CanGoBack        bool
	SummaryError     string
	Errors           map[string]string
	Values           map[string]string
	Selections       map[string]map[string]bool
	PublicID         string
	FatalError       string
	TurnstileSiteKey string
}

func publicAssetHandler() http.Handler {
	directory, err := fs.Sub(publicAssets, "public_assets")
	if err != nil {
		panic(err)
	}
	files := http.StripPrefix("/public-assets/", http.FileServerFS(directory))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assetName := strings.TrimPrefix(r.URL.Path, "/public-assets/")
		if r.URL.Query().Get("v") == publicAssetVersions[assetName] {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "public, max-age=0, must-revalidate")
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		files.ServeHTTP(w, r)
	})
}

func (s *Server) publicLanding(w http.ResponseWriter, r *http.Request) {
	if !s.publicLimiter.Allow(s.requestRemoteIP(r), r.PathValue("slug"), "landing", 120, 12_000) {
		publicTooManyRequests(w)
		return
	}
	project, startToken, err := s.publicForms.LoadLanding(r.Context(), r.PathValue("slug"))
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, publicform.ErrUnavailable) {
			status = http.StatusNotFound
		} else if errors.Is(err, publicform.ErrTurnstileUnavailable) {
			status = http.StatusServiceUnavailable
		}
		s.renderPublicError(w, r.PathValue("slug"), err, status)
		return
	}
	view := basePublicView(project, s.publicForms.TurnstileSiteKey())
	view.Stage = "landing"
	view.StartToken = startToken
	view.Closed = project.Status != "published"
	s.renderPublic(w, http.StatusOK, view, project.Version.Checksum)
}

func (s *Server) publicStart(w http.ResponseWriter, r *http.Request) {
	if !s.publicLimiter.Allow(s.requestRemoteIP(r), r.PathValue("slug"), "start", 30, 3_000) {
		publicTooManyRequests(w)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseForm(); err != nil {
		s.renderPublicError(w, r.PathValue("slug"), errors.New("درخواست شروع معتبر نیست"), http.StatusBadRequest)
		return
	}
	session, token, err := s.publicForms.Start(
		r.Context(), r.PathValue("slug"), r.Form.Get("start_token"), r.Form.Get("website"),
		r.Form.Get("cf-turnstile-response"), s.requestRemoteIP(r),
	)
	if err != nil {
		status := http.StatusUnprocessableEntity
		if errors.Is(err, publicform.ErrUnavailable) {
			status = http.StatusConflict
		}
		s.renderPublicError(w, r.PathValue("slug"), err, status)
		return
	}
	navigation := publicform.Navigation{Session: session, Page: findPublicPage(session.Project.Version.Schema, session.CurrentPageID)}
	s.renderNavigation(w, navigation, token, nil, nil, http.StatusOK)
}

func (s *Server) publicStep(w http.ResponseWriter, r *http.Request) {
	if !s.publicLimiter.Allow(s.requestRemoteIP(r), r.PathValue("slug"), "step", 180, 18_000) {
		publicTooManyRequests(w)
		return
	}
	values, uploadProblems, err := s.parsePublicMultipart(w, r)
	if err != nil {
		s.renderPublicError(w, r.PathValue("slug"), err, http.StatusRequestEntityTooLarge)
		return
	}
	token := values.Get("session_token")
	if values.Get("_action") == "back" {
		navigation, err := s.publicForms.Back(r.Context(), r.PathValue("slug"), token)
		if err != nil {
			s.renderPublicError(w, r.PathValue("slug"), err, http.StatusConflict)
			return
		}
		s.renderNavigation(w, navigation, token, nil, nil, http.StatusOK)
		return
	}
	if len(uploadProblems) > 0 {
		navigation, resumeErr := s.publicForms.Resume(r.Context(), r.PathValue("slug"), token)
		if resumeErr != nil {
			s.renderPublicError(w, r.PathValue("slug"), resumeErr, http.StatusConflict)
			return
		}
		s.renderNavigation(w, navigation, token, values, uploadProblems, http.StatusUnprocessableEntity)
		return
	}
	navigation, err := s.publicForms.SubmitPage(r.Context(), r.PathValue("slug"), token, values.Get("page_id"), values)
	if err != nil {
		var answerErr *publicform.AnswerError
		if errors.As(err, &answerErr) {
			s.renderNavigation(w, navigation, token, values, answerErr.Problems, http.StatusUnprocessableEntity)
			return
		}
		s.renderPublicError(w, r.PathValue("slug"), err, http.StatusConflict)
		return
	}
	s.renderNavigation(w, navigation, token, nil, nil, http.StatusOK)
}

func (s *Server) parsePublicMultipart(w http.ResponseWriter, r *http.Request) (url.Values, []publicform.AnswerProblem, error) {
	r.Body = http.MaxBytesReader(w, r.Body, s.publicForms.MaxRequestBytes())
	reader, err := r.MultipartReader()
	if err != nil {
		return nil, nil, errors.New("قالب درخواست فرم معتبر نیست")
	}
	values := make(url.Values)
	problems := make([]publicform.AnswerProblem, 0)
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, nil, errors.New("خواندن درخواست فرم کامل نشد")
		}
		name := part.FormName()
		if name == "" {
			part.Close()
			continue
		}
		if part.FileName() == "" {
			contents, err := io.ReadAll(io.LimitReader(part, (4<<20)+1))
			part.Close()
			if err != nil || len(contents) > 4<<20 {
				return nil, nil, errors.New("مقدار یکی از فیلدها بیش از حد بزرگ است")
			}
			values.Add(name, string(contents))
			continue
		}
		token, pageID := values.Get("session_token"), values.Get("page_id")
		fieldID := strings.TrimSuffix(name, "_file")
		if token == "" || pageID == "" {
			part.Close()
			return nil, nil, errors.New("ترتیب بخش‌های درخواست معتبر نیست")
		}
		_, stageErr := s.publicForms.StageUpload(r.Context(), r.PathValue("slug"), token, pageID, fieldID, part.FileName(), part)
		part.Close()
		if stageErr != nil {
			problems = append(problems, publicform.AnswerProblem{FieldID: fieldID, Message: stageErr.Error()})
		}
	}
	for fieldID, entries := range values {
		if len(entries) == 0 || !strings.HasPrefix(entries[0], "data:image/png;base64,") {
			continue
		}
		if _, err := s.publicForms.StageSignature(r.Context(), r.PathValue("slug"), values.Get("session_token"), values.Get("page_id"), fieldID, entries[0]); err != nil {
			problems = append(problems, publicform.AnswerProblem{FieldID: fieldID, Message: err.Error()})
		}
		delete(values, fieldID)
	}
	return values, problems, nil
}

func (s *Server) publicSubmit(w http.ResponseWriter, r *http.Request) {
	if !s.publicLimiter.Allow(s.requestRemoteIP(r), r.PathValue("slug"), "submit", 30, 3_000) {
		publicTooManyRequests(w)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseForm(); err != nil {
		s.renderPublicError(w, r.PathValue("slug"), errors.New("درخواست ثبت معتبر نیست"), http.StatusBadRequest)
		return
	}
	submission, _, _, err := s.publicForms.Submit(
		r.Context(), r.PathValue("slug"), r.Form.Get("session_token"), r.Form.Get("idempotency_key"), s.requestFingerprint(r),
	)
	if err != nil {
		status := http.StatusConflict
		if errors.Is(err, publicform.ErrTooFast) {
			status = http.StatusUnprocessableEntity
		}
		s.renderPublicError(w, r.PathValue("slug"), err, status)
		return
	}
	if submission.Theme == "" {
		submission.Theme = "minimal"
	}
	if submission.Alignment == "" {
		submission.Alignment = "start"
	}
	s.renderPublic(w, http.StatusOK, publicView{
		Stage: "success", Title: "پاسخ ثبت شد", Slug: r.PathValue("slug"), Theme: submission.Theme, Alignment: submission.Alignment, PublicID: submission.PublicID,
	}, "")
}

func (s *Server) renderNavigation(w http.ResponseWriter, navigation publicform.Navigation, token string, values url.Values, problems []publicform.AnswerProblem, status int) {
	view := basePublicView(navigation.Session.Project, s.publicForms.TurnstileSiteKey())
	view.SessionToken = token
	view.Page = navigation.Page
	view.Ending = navigation.Ending
	view.CanGoBack = len(navigation.Session.History) > 0
	view.TotalSteps = len(navigation.Session.Project.Version.Schema.Pages)
	view.CurrentStep = len(navigation.Session.History) + 1
	if view.TotalSteps > 0 {
		view.ProgressPercent = view.CurrentStep * 100 / view.TotalSteps
		if view.ProgressPercent > 100 {
			view.ProgressPercent = 100
		}
	}
	view.Values, view.Selections = templateValues(values, navigation.Session.Answers, navigation.Session.Project.Version.Schema, s.timezone)
	view.Errors = make(map[string]string)
	for _, problem := range problems {
		view.Errors[problem.FieldID] = problem.Message
	}
	if len(problems) > 0 {
		view.SummaryError = "موارد علامت‌گذاری‌شده را اصلاح کنید."
	}
	if navigation.Ending != nil {
		view.Stage = "ending"
		view.CurrentStep = view.TotalSteps
		view.ProgressPercent = 100
		key, err := id.Token(24)
		if err != nil {
			s.renderPublicError(w, view.Slug, err, http.StatusInternalServerError)
			return
		}
		view.SubmitKey = key
	} else {
		view.Stage = "page"
		if navigation.Page != nil {
			view.Fields = navigation.Page.Fields
		}
	}
	s.renderPublic(w, status, view, navigation.Session.Project.Version.Checksum)
}

func (s *Server) renderPublicError(w http.ResponseWriter, slug string, err error, status int) {
	message := "فرم در دسترس نیست."
	if status < 500 {
		message = err.Error()
	} else {
		s.logger.Error("public form request failed", "error", err)
	}
	s.renderPublic(w, status, publicView{Stage: "error", Title: "فرم در دسترس نیست", Slug: slug, Theme: "minimal", Alignment: "start", FatalError: message}, "")
}

func (s *Server) renderPublic(w http.ResponseWriter, status int, view publicView, checksum string) {
	var rendered bytes.Buffer
	if err := publicTemplate.ExecuteTemplate(&rendered, "public", view); err != nil {
		s.logger.Error("render public form", "error", err)
		http.Error(w, "نمایش فرم ممکن نشد.", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if checksum != "" {
		w.Header().Set("ETag", `"`+checksum+`"`)
	}
	w.WriteHeader(status)
	_, _ = w.Write(rendered.Bytes())
}

func basePublicView(project publicform.PublishedProject, turnstileSiteKey string) publicView {
	theme := project.Version.Schema.Theme.Preset
	if theme == "" {
		theme = "minimal"
	}
	alignment := project.Version.Schema.Theme.Alignment
	if alignment == "" {
		alignment = "start"
	}
	if !project.Version.Schema.PublicSettings.TurnstileEnabled {
		turnstileSiteKey = ""
	}
	return publicView{
		Title: project.Title, Slug: project.Slug, Theme: theme, Alignment: alignment,
		VersionLabel: "نسخهٔ " + persianNumber(project.Version.VersionNumber), TurnstileSiteKey: turnstileSiteKey,
		Errors: map[string]string{}, Values: map[string]string{}, Selections: map[string]map[string]bool{},
	}
}

func templateValues(submitted url.Values, stored map[string]any, schema form.Schema, timezone string) (map[string]string, map[string]map[string]bool) {
	values := make(map[string]string)
	selections := make(map[string]map[string]bool)
	for key, items := range submitted {
		if len(items) > 0 {
			values[key] = items[0]
		}
		selections[key] = make(map[string]bool)
		for _, item := range items {
			selections[key][item] = true
		}
	}
	if submitted != nil {
		return values, selections
	}
	fields := make(map[string]form.Field)
	for _, page := range schema.Pages {
		for _, field := range page.Fields {
			fields[field.ID] = field
		}
	}
	location, _ := time.LoadLocation(timezone)
	for key, value := range stored {
		field := fields[key]
		switch typed := value.(type) {
		case string:
			values[key] = publicDisplayString(field, typed, location)
		case float64:
			values[key] = strconv.FormatFloat(typed, 'f', -1, 64)
		case bool:
			values[key] = strconv.FormatBool(typed)
		case []any:
			selections[key] = make(map[string]bool)
			for _, item := range typed {
				if text, ok := item.(string); ok {
					selections[key][text] = true
				}
			}
		case map[string]any:
			switch field.Type {
			case form.FieldFullName:
				values[key+"_first"], _ = typed["first"].(string)
				values[key+"_last"], _ = typed["last"].(string)
			case form.FieldAddress:
				values[key+"_province"], _ = typed["province"].(string)
				values[key+"_city"], _ = typed["city"].(string)
				values[key+"_address"], _ = typed["address"].(string)
				values[key+"_postal"], _ = typed["postal_code"].(string)
			case form.FieldDateRange:
				if start, ok := typed["start"].(string); ok {
					values[key+"_start"] = publicDisplayDate(start)
				}
				if end, ok := typed["end"].(string); ok {
					values[key+"_end"] = publicDisplayDate(end)
				}
			case form.FieldMatrix:
				for rowID, selected := range typed {
					selections[rowID] = make(map[string]bool)
					switch rowValue := selected.(type) {
					case string:
						selections[rowID][rowValue] = true
					case []any:
						for _, item := range rowValue {
							if text, ok := item.(string); ok {
								selections[rowID][text] = true
							}
						}
					}
				}
			}
		}
	}
	return values, selections
}

func publicDisplayString(field form.Field, value string, location *time.Location) string {
	switch field.Type {
	case form.FieldPersianDate:
		return publicDisplayDate(value)
	case form.FieldDateTime:
		instant, err := time.Parse(time.RFC3339, value)
		if err != nil || location == nil {
			return value
		}
		local := instant.In(location)
		date, err := persiandate.FromGregorian(local)
		if err != nil {
			return value
		}
		return date.PersianString() + " " + persianNumber(local.Format("15:04"))
	default:
		return value
	}
}

func publicDisplayDate(value string) string {
	parsed, err := time.Parse(time.DateOnly, value)
	if err != nil {
		return value
	}
	date, err := persiandate.FromGregorian(parsed)
	if err != nil {
		return value
	}
	return date.PersianString()
}

func (s *Server) requestFingerprint(r *http.Request) string {
	host := s.requestRemoteIP(r)
	userAgent := r.UserAgent()
	userAgent = strings.ToValidUTF8(userAgent, "�")
	if len(userAgent) > 200 {
		userAgent = userAgent[:200]
		for !utf8.ValidString(userAgent) {
			userAgent = userAgent[:len(userAgent)-1]
		}
	}
	return host + "|" + userAgent
}

func directRemoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return strings.TrimSpace(host)
}

func (s *Server) requestRemoteIP(r *http.Request) string {
	directText := directRemoteIP(r)
	direct, err := netip.ParseAddr(directText)
	if err != nil || !s.isTrustedProxy(direct) {
		return directText
	}
	current := direct.Unmap()
	parts := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	for index := len(parts) - 1; index >= 0 && s.isTrustedProxy(current); index-- {
		candidate, parseErr := netip.ParseAddr(strings.TrimSpace(parts[index]))
		if parseErr != nil {
			break
		}
		current = candidate.Unmap()
	}
	return current.String()
}

func (s *Server) isTrustedProxy(address netip.Addr) bool {
	address = address.Unmap()
	for _, prefix := range s.trustedProxies {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

func findPublicPage(schema form.Schema, pageID string) *form.Page {
	for index := range schema.Pages {
		if schema.Pages[index].ID == pageID {
			return &schema.Pages[index]
		}
	}
	return nil
}

func persianNumber(value any) string {
	text := fmt.Sprint(value)
	return strings.NewReplacer("0", "۰", "1", "۱", "2", "۲", "3", "۳", "4", "۴", "5", "۵", "6", "۶", "7", "۷", "8", "۸", "9", "۹").Replace(text)
}

func publicTooManyRequests(w http.ResponseWriter) {
	w.Header().Set("Retry-After", "60")
	http.Error(w, "تعداد درخواست‌ها بیش از حد مجاز است. کمی بعد دوباره تلاش کنید.", http.StatusTooManyRequests)
}
