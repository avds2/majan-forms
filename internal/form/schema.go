package form

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"forms/internal/id"
	"forms/internal/persiandate"
)

const SchemaVersion = 1

type FieldType string

const (
	FieldShortText     FieldType = "short_text"
	FieldLongText      FieldType = "long_text"
	FieldEmail         FieldType = "email"
	FieldPhone         FieldType = "phone"
	FieldURL           FieldType = "url"
	FieldFullName      FieldType = "full_name"
	FieldAddress       FieldType = "address"
	FieldNationalID    FieldType = "national_id"
	FieldNumber        FieldType = "number"
	FieldMoney         FieldType = "money"
	FieldPercent       FieldType = "percent"
	FieldRating        FieldType = "rating"
	FieldScale         FieldType = "scale"
	FieldNPS           FieldType = "nps"
	FieldHidden        FieldType = "hidden"
	FieldCalculated    FieldType = "calculated"
	FieldYesNo         FieldType = "yes_no"
	FieldSingleChoice  FieldType = "single_choice"
	FieldMultiChoice   FieldType = "multi_choice"
	FieldDropdown      FieldType = "dropdown"
	FieldMultiDropdown FieldType = "multi_dropdown"
	FieldMatrix        FieldType = "matrix"
	FieldPersianDate   FieldType = "persian_date"
	FieldTime          FieldType = "time"
	FieldDateTime      FieldType = "datetime"
	FieldDateRange     FieldType = "date_range"
	FieldFile          FieldType = "file"
	FieldSignature     FieldType = "signature"
	FieldConsent       FieldType = "consent"
	FieldHeading       FieldType = "heading"
	FieldDescription   FieldType = "description"
	FieldDivider       FieldType = "divider"
	FieldImage         FieldType = "image"
	FieldSpacer        FieldType = "spacer"
)

const MaxUploadBytes int64 = 100 << 20

var (
	ErrInvalidSchema = errors.New("ساختار فرم معتبر نیست")
	uuidPattern      = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
)

type Schema struct {
	SchemaVersion  int             `json:"schema_version"`
	Title          string          `json:"title"`
	Pages          []Page          `json:"pages"`
	Endings        []Ending        `json:"endings"`
	Flow           Flow            `json:"flow"`
	Theme          Theme           `json:"theme"`
	PublicSettings PublicSettings  `json:"public_settings"`
	EditorMetadata json.RawMessage `json:"editor_metadata"`
}

type Page struct {
	ID          string  `json:"id"`
	Title       string  `json:"title"`
	Description string  `json:"description,omitempty"`
	Fields      []Field `json:"fields"`
}

type Ending struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Message string `json:"message,omitempty"`
}

type Field struct {
	ID           string       `json:"id"`
	Type         FieldType    `json:"type"`
	Label        string       `json:"label"`
	HelpText     string       `json:"help_text,omitempty"`
	Required     bool         `json:"required,omitempty"`
	Placeholder  string       `json:"placeholder,omitempty"`
	Width        string       `json:"width,omitempty"`
	Content      string       `json:"content,omitempty"`
	DefaultValue any          `json:"default_value,omitempty"`
	Options      []Option     `json:"options,omitempty"`
	Text         *TextRule    `json:"text,omitempty"`
	Number       *NumberRule  `json:"number,omitempty"`
	Date         *DateRule    `json:"date,omitempty"`
	Scale        *ScaleRule   `json:"scale,omitempty"`
	Matrix       *MatrixRule  `json:"matrix,omitempty"`
	File         *FileRule    `json:"file,omitempty"`
	Consent      *ConsentRule `json:"consent,omitempty"`
	Image        *ImageRule   `json:"image,omitempty"`
}

type Option struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type NumberRule struct {
	Minimum *float64 `json:"minimum,omitempty"`
	Maximum *float64 `json:"maximum,omitempty"`
	Integer bool     `json:"integer,omitempty"`
}

type TextRule struct {
	MinimumLength int    `json:"minimum_length,omitempty"`
	MaximumLength int    `json:"maximum_length,omitempty"`
	Pattern       string `json:"pattern,omitempty"`
	NationalID    bool   `json:"national_id,omitempty"`
}

type DateRule struct {
	Minimum string `json:"minimum,omitempty"`
	Maximum string `json:"maximum,omitempty"`
}

type ScaleRule struct {
	Minimum   int    `json:"minimum"`
	Maximum   int    `json:"maximum"`
	LowLabel  string `json:"low_label,omitempty"`
	HighLabel string `json:"high_label,omitempty"`
}

type MatrixRule struct {
	Rows     []Option `json:"rows"`
	Columns  []Option `json:"columns"`
	Multiple bool     `json:"multiple,omitempty"`
}

type FileRule struct {
	MaximumBytes int64    `json:"maximum_bytes"`
	MaximumFiles int      `json:"maximum_files"`
	Extensions   []string `json:"extensions"`
}

type ConsentRule struct {
	AgreementText string `json:"agreement_text"`
	LinkLabel     string `json:"link_label,omitempty"`
	LinkURL       string `json:"link_url,omitempty"`
}

type ImageRule struct {
	Source string `json:"source"`
	Alt    string `json:"alt"`
}

type Theme struct {
	Preset       string `json:"preset"`
	PrimaryColor string `json:"primary_color,omitempty"`
	Alignment    string `json:"alignment,omitempty"`
}

type PublicSettings struct {
	MinimumFillSeconds int  `json:"minimum_fill_seconds"`
	TurnstileEnabled   bool `json:"turnstile_enabled,omitempty"`
}

type Flow struct {
	StartPageID string     `json:"start_page_id"`
	Edges       []FlowEdge `json:"edges"`
}

type FlowEdge struct {
	ID        string             `json:"id"`
	SourceID  string             `json:"source_id"`
	TargetID  string             `json:"target_id"`
	Priority  int                `json:"priority"`
	Fallback  bool               `json:"fallback,omitempty"`
	Condition *ConditionGroup    `json:"condition,omitempty"`
	Actions   []AssignmentAction `json:"actions,omitempty"`
}

type ConditionGroup struct {
	Match      string           `json:"match"`
	Conditions []Condition      `json:"conditions,omitempty"`
	Groups     []ConditionGroup `json:"groups,omitempty"`
}

type Condition struct {
	FieldID  string `json:"field_id"`
	Operator string `json:"operator"`
	Value    any    `json:"value,omitempty"`
}

type AssignmentAction struct {
	ID            string `json:"id"`
	TargetFieldID string `json:"target_field_id"`
	Mode          string `json:"mode"`
	Source        string `json:"source"`
	SourceFieldID string `json:"source_field_id,omitempty"`
	Value         any    `json:"value,omitempty"`
}

type ValidationProblem struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

type SchemaError struct{ Problems []ValidationProblem }

func (e *SchemaError) Error() string { return ErrInvalidSchema.Error() }
func (e *SchemaError) Unwrap() error { return ErrInvalidSchema }

func NewEmpty(title string) (Schema, error) {
	pageID, err := id.New()
	if err != nil {
		return Schema{}, err
	}
	endingID, err := id.New()
	if err != nil {
		return Schema{}, err
	}
	edgeID, err := id.New()
	if err != nil {
		return Schema{}, err
	}
	return Schema{
		SchemaVersion: SchemaVersion,
		Title:         strings.TrimSpace(title),
		Pages: []Page{{
			ID:     pageID,
			Title:  "صفحهٔ نخست",
			Fields: []Field{},
		}},
		Endings: []Ending{{
			ID:      endingID,
			Title:   "پایان اصلی",
			Message: "سپاس از وقتی که گذاشتید.",
		}},
		Flow: Flow{
			StartPageID: pageID,
			Edges: []FlowEdge{{
				ID: edgeID, SourceID: pageID, TargetID: endingID, Priority: 1000, Fallback: true,
			}},
		},
		Theme:          Theme{Preset: "minimal", Alignment: "start"},
		PublicSettings: PublicSettings{MinimumFillSeconds: 2},
		EditorMetadata: json.RawMessage(`{}`),
	}, nil
}

func (s Schema) Validate() error {
	problems := make([]ValidationProblem, 0)
	add := func(path, message string) {
		problems = append(problems, ValidationProblem{Path: path, Message: message})
	}
	if s.SchemaVersion != SchemaVersion {
		add("schema_version", "نسخهٔ ساختار پشتیبانی نمی‌شود")
	}
	if strings.TrimSpace(s.Title) == "" || utf8.RuneCountInString(s.Title) > 200 {
		add("title", "عنوان فرم الزامی و حداکثر ۲۰۰ نویسه است")
	}
	if len(s.Pages) < 1 || len(s.Pages) > 200 {
		add("pages", "فرم باید بین ۱ تا ۲۰۰ صفحه داشته باشد")
	}
	if len(s.Endings) < 1 || len(s.Endings) > 20 {
		add("endings", "فرم باید دست‌کم یک پایان داشته باشد")
	}
	ids := make(map[string]string)
	checkID := func(path, value string) {
		if !uuidPattern.MatchString(value) {
			add(path, "شناسه معتبر نیست")
			return
		}
		if previous, exists := ids[value]; exists {
			add(path, "شناسه با "+previous+" تکراری است")
			return
		}
		ids[value] = path
	}
	fieldCount := 0
	for pageIndex, page := range s.Pages {
		pagePath := fmt.Sprintf("pages[%d]", pageIndex)
		checkID(pagePath+".id", page.ID)
		if strings.TrimSpace(page.Title) == "" || utf8.RuneCountInString(page.Title) > 160 {
			add(pagePath+".title", "عنوان صفحه الزامی است")
		}
		if utf8.RuneCountInString(page.Description) > 5000 {
			add(pagePath+".description", "توضیح صفحه بیش از حد بلند است")
		}
		for fieldIndex, field := range page.Fields {
			fieldCount++
			path := fmt.Sprintf("%s.fields[%d]", pagePath, fieldIndex)
			checkID(path+".id", field.ID)
			validateField(field, path, checkID, add)
		}
	}
	if fieldCount > 1000 {
		add("pages", "فرم بیش از ۱۰۰۰ فیلد دارد")
	}
	for endingIndex, ending := range s.Endings {
		path := fmt.Sprintf("endings[%d]", endingIndex)
		checkID(path+".id", ending.ID)
		if strings.TrimSpace(ending.Title) == "" || utf8.RuneCountInString(ending.Title) > 160 {
			add(path+".title", "عنوان پایان الزامی است")
		}
		if utf8.RuneCountInString(ending.Message) > 5000 {
			add(path+".message", "پیام پایان بیش از حد بلند است")
		}
	}
	if len(s.Flow.Edges) > 500 {
		add("flow.edges", "تعداد مسیرها نمی‌تواند بیش از ۵۰۰ باشد")
	}
	for edgeIndex, edge := range s.Flow.Edges {
		path := fmt.Sprintf("flow.edges[%d]", edgeIndex)
		checkID(path+".id", edge.ID)
		if edge.Priority < 0 || edge.Priority > 1_000_000 {
			add(path+".priority", "اولویت مسیر معتبر نیست")
		}
		if edge.Condition != nil {
			validateConditionGroupShape(*edge.Condition, path+".condition", 0, add)
		}
		if len(edge.Actions) > 50 {
			add(path+".actions", "تعداد تغییر مقدار در یک مسیر بیش از حد است")
		}
		for actionIndex, action := range edge.Actions {
			actionPath := fmt.Sprintf("%s.actions[%d]", path, actionIndex)
			checkID(actionPath+".id", action.ID)
			if action.Mode != "replace" && action.Mode != "if_empty" {
				add(actionPath+".mode", "حالت تغییر مقدار معتبر نیست")
			}
			if action.Source != "constant" && action.Source != "copy" {
				add(actionPath+".source", "منبع تغییر مقدار معتبر نیست")
			}
		}
	}
	if s.Theme.Preset != "" && s.Theme.Preset != "minimal" && s.Theme.Preset != "formal" && s.Theme.Preset != "warm" {
		add("theme.preset", "قالب ظاهری معتبر نیست")
	}
	if s.Theme.PrimaryColor != "" && !regexp.MustCompile(`^#[0-9a-fA-F]{6}$`).MatchString(s.Theme.PrimaryColor) {
		add("theme.primary_color", "رنگ اصلی باید کد شش‌رقمی باشد")
	}
	if s.Theme.Alignment != "" && s.Theme.Alignment != "start" && s.Theme.Alignment != "center" {
		add("theme.alignment", "تراز فرم معتبر نیست")
	}
	if s.PublicSettings.MinimumFillSeconds < 0 || s.PublicSettings.MinimumFillSeconds > 3600 {
		add("public_settings.minimum_fill_seconds", "حداقل زمان تکمیل معتبر نیست")
	}
	if len(s.EditorMetadata) > 256*1024 {
		add("editor_metadata", "اطلاعات ویرایشگر بیش از حد بزرگ است")
	} else if len(s.EditorMetadata) > 0 && !json.Valid(s.EditorMetadata) {
		add("editor_metadata", "اطلاعات ویرایشگر JSON معتبر نیست")
	}
	if len(problems) > 0 {
		return &SchemaError{Problems: problems}
	}
	return nil
}

func validateField(field Field, path string, checkID func(string, string), add func(string, string)) {
	allowed := map[FieldType]bool{
		FieldShortText: true, FieldLongText: true, FieldEmail: true, FieldPhone: true,
		FieldURL: true, FieldFullName: true, FieldAddress: true, FieldNationalID: true,
		FieldNumber: true, FieldMoney: true, FieldPercent: true, FieldRating: true,
		FieldScale: true, FieldNPS: true, FieldHidden: true, FieldCalculated: true,
		FieldYesNo: true, FieldSingleChoice: true, FieldMultiChoice: true,
		FieldDropdown: true, FieldMultiDropdown: true, FieldMatrix: true,
		FieldPersianDate: true, FieldTime: true, FieldDateTime: true, FieldDateRange: true,
		FieldFile: true, FieldSignature: true, FieldConsent: true, FieldHeading: true,
		FieldDescription: true, FieldDivider: true, FieldImage: true, FieldSpacer: true,
	}
	if !allowed[field.Type] {
		add(path+".type", "نوع فیلد پشتیبانی نمی‌شود")
	}
	displayOnly := field.Type == FieldHeading || field.Type == FieldDescription || field.Type == FieldDivider || field.Type == FieldImage || field.Type == FieldSpacer
	if field.Type == FieldHeading || field.Type == FieldDescription {
		if strings.TrimSpace(field.Content) == "" || utf8.RuneCountInString(field.Content) > 5000 {
			add(path+".content", "محتوای نمایشی الزامی و حداکثر ۵۰۰۰ نویسه است")
		}
	} else if !displayOnly && (strings.TrimSpace(field.Label) == "" || utf8.RuneCountInString(field.Label) > 300) {
		add(path+".label", "عنوان فیلد الزامی است")
	}
	if utf8.RuneCountInString(field.HelpText) > 1000 {
		add(path+".help_text", "متن راهنما بیش از حد بلند است")
	}
	if utf8.RuneCountInString(field.Placeholder) > 300 {
		add(path+".placeholder", "متن جای‌نگهدار بیش از حد بلند است")
	}
	if field.Width != "" && field.Width != "full" && field.Width != "half" {
		add(path+".width", "عرض فیلد معتبر نیست")
	}
	if isChoiceField(field.Type) {
		if len(field.Options) < 2 || len(field.Options) > 200 {
			add(path+".options", "فیلد انتخاب باید بین ۲ تا ۲۰۰ گزینه داشته باشد")
		}
		for index, option := range field.Options {
			optionPath := fmt.Sprintf("%s.options[%d]", path, index)
			checkID(optionPath+".id", option.ID)
			if strings.TrimSpace(option.Label) == "" {
				add(optionPath+".label", "عنوان گزینه الزامی است")
			} else if utf8.RuneCountInString(option.Label) > 300 {
				add(optionPath+".label", "عنوان گزینه بیش از حد بلند است")
			}
		}
	}
	if field.Type == FieldMatrix {
		if field.Matrix == nil || len(field.Matrix.Rows) < 1 || len(field.Matrix.Rows) > 50 || len(field.Matrix.Columns) < 2 || len(field.Matrix.Columns) > 50 {
			add(path+".matrix", "ماتریس باید ۱ تا ۵۰ سطر و ۲ تا ۵۰ ستون داشته باشد")
		} else {
			for index, item := range append(append([]Option{}, field.Matrix.Rows...), field.Matrix.Columns...) {
				itemPath := fmt.Sprintf("%s.matrix.items[%d]", path, index)
				checkID(itemPath+".id", item.ID)
				if strings.TrimSpace(item.Label) == "" || utf8.RuneCountInString(item.Label) > 200 {
					add(itemPath+".label", "عنوان سطر یا ستون معتبر نیست")
				}
			}
		}
	}
	if field.Text != nil {
		if field.Text.MinimumLength < 0 || field.Text.MaximumLength < 0 || field.Text.MaximumLength > 100_000 || (field.Text.MaximumLength > 0 && field.Text.MinimumLength > field.Text.MaximumLength) {
			add(path+".text", "محدودهٔ طول متن معتبر نیست")
		}
		if len(field.Text.Pattern) > 256 {
			add(path+".text.pattern", "الگوی متن بیش از حد بلند است")
		} else if field.Text.Pattern != "" {
			if _, err := regexp.Compile(field.Text.Pattern); err != nil {
				add(path+".text.pattern", "الگوی متن معتبر نیست")
			}
		}
	}
	if isNumberField(field.Type) && field.Number != nil && field.Number.Minimum != nil && field.Number.Maximum != nil && *field.Number.Minimum > *field.Number.Maximum {
		add(path+".number", "کمینه نمی‌تواند از بیشینه بزرگ‌تر باشد")
	}
	if field.Type == FieldPercent && field.Number != nil {
		if field.Number.Minimum != nil && *field.Number.Minimum < 0 || field.Number.Maximum != nil && *field.Number.Maximum > 100 {
			add(path+".number", "درصد باید در بازهٔ صفر تا صد باشد")
		}
	}
	if field.Type == FieldRating || field.Type == FieldScale || field.Type == FieldNPS {
		minimum, maximum := 1, 5
		if field.Type == FieldNPS {
			minimum, maximum = 0, 10
		}
		if field.Scale != nil {
			minimum, maximum = field.Scale.Minimum, field.Scale.Maximum
		}
		if minimum < 0 || maximum > 100 || minimum >= maximum || maximum-minimum > 20 {
			add(path+".scale", "محدودهٔ سنجش معتبر نیست")
		}
	}
	if isDateField(field.Type) && field.Date != nil {
		var minDate, maxDate time.Time
		var minOK, maxOK bool
		if field.Date.Minimum != "" {
			var err error
			minDate, err = time.Parse(time.DateOnly, field.Date.Minimum)
			minOK = err == nil
			if err == nil {
				_, err = persiandate.FromGregorian(minDate)
				minOK = err == nil
			}
			if !minOK {
				add(path+".date.minimum", "کمینهٔ تاریخ معتبر نیست")
			}
		}
		if field.Date.Maximum != "" {
			var err error
			maxDate, err = time.Parse(time.DateOnly, field.Date.Maximum)
			maxOK = err == nil
			if err == nil {
				_, err = persiandate.FromGregorian(maxDate)
				maxOK = err == nil
			}
			if !maxOK {
				add(path+".date.maximum", "بیشینهٔ تاریخ معتبر نیست")
			}
		}
		if minOK && maxOK {
			if minDate.After(maxDate) {
				add(path+".date", "کمینهٔ تاریخ نمی‌تواند پس از بیشینه باشد")
			}
		}
	}
	if field.Type == FieldFile {
		validateFileRule(field.File, path+".file", add)
	}
	if field.Type == FieldConsent {
		if field.Consent == nil || strings.TrimSpace(field.Consent.AgreementText) == "" || utf8.RuneCountInString(field.Consent.AgreementText) > 2000 {
			add(path+".consent", "متن رضایت الزامی و حداکثر ۲۰۰۰ نویسه است")
		} else if field.Consent.LinkURL != "" && !safeHTTPSURL(field.Consent.LinkURL) {
			add(path+".consent.link_url", "پیوند رضایت باید HTTPS معتبر باشد")
		}
	}
	if field.Type == FieldImage {
		if field.Image == nil || !safeHTTPSURL(field.Image.Source) || strings.TrimSpace(field.Image.Alt) == "" {
			add(path+".image", "تصویر باید نشانی HTTPS و متن جایگزین داشته باشد")
		}
	}
}

func isChoiceField(fieldType FieldType) bool {
	return fieldType == FieldSingleChoice || fieldType == FieldMultiChoice || fieldType == FieldDropdown || fieldType == FieldMultiDropdown
}

func isNumberField(fieldType FieldType) bool {
	return fieldType == FieldNumber || fieldType == FieldMoney || fieldType == FieldPercent || fieldType == FieldRating || fieldType == FieldScale || fieldType == FieldNPS || fieldType == FieldCalculated
}

func isDateField(fieldType FieldType) bool {
	return fieldType == FieldPersianDate || fieldType == FieldDateTime || fieldType == FieldDateRange
}

func validateFileRule(rule *FileRule, path string, add func(string, string)) {
	if rule == nil || rule.MaximumBytes < 1 || rule.MaximumBytes > MaxUploadBytes || rule.MaximumFiles < 1 || rule.MaximumFiles > 20 {
		add(path, "محدودیت تعداد یا حجم فایل معتبر نیست")
		return
	}
	allowed := map[string]bool{"pdf": true, "jpg": true, "jpeg": true, "png": true, "webp": true, "txt": true, "csv": true, "docx": true, "xlsx": true, "zip": true}
	if len(rule.Extensions) < 1 || len(rule.Extensions) > len(allowed) {
		add(path+".extensions", "پسوندهای مجاز فایل معتبر نیستند")
		return
	}
	seen := make(map[string]bool)
	for _, extension := range rule.Extensions {
		extension = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(extension)), ".")
		if !allowed[extension] || seen[extension] {
			add(path+".extensions", "پسوند فایل ناامن، ناشناخته یا تکراری است")
			return
		}
		seen[extension] = true
	}
}

func safeHTTPSURL(value string) bool {
	parsed, err := url.ParseRequestURI(strings.TrimSpace(value))
	return err == nil && parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil
}

func validateConditionGroupShape(group ConditionGroup, path string, depth int, add func(string, string)) {
	if depth > 5 {
		add(path, "عمق گروه شرط بیش از حد است")
		return
	}
	if group.Match != "all" && group.Match != "any" {
		add(path+".match", "نوع گروه شرط معتبر نیست")
	}
	if len(group.Conditions)+len(group.Groups) < 1 || len(group.Conditions)+len(group.Groups) > 50 {
		add(path, "گروه شرط باید بین ۱ تا ۵۰ عضو داشته باشد")
	}
	for index, condition := range group.Conditions {
		if strings.TrimSpace(condition.FieldID) == "" || strings.TrimSpace(condition.Operator) == "" {
			add(fmt.Sprintf("%s.conditions[%d]", path, index), "شرط کامل نیست")
		}
	}
	for index, child := range group.Groups {
		validateConditionGroupShape(child, fmt.Sprintf("%s.groups[%d]", path, index), depth+1, add)
	}
}

func EncodeAndChecksum(schema Schema) ([]byte, string, error) {
	if err := schema.Validate(); err != nil {
		return nil, "", err
	}
	encoded, err := json.Marshal(schema)
	if err != nil {
		return nil, "", fmt.Errorf("encode form schema: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return encoded, hex.EncodeToString(digest[:]), nil
}
