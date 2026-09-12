package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"forms/internal/form"
)

func TestPublicAssetHandlerCachePolicy(t *testing.T) {
	handler := publicAssetHandler()

	versioned := httptest.NewRecorder()
	versionedRequest := httptest.NewRequest(http.MethodGet, "/public-assets/forms.js?v="+publicAssetVersions["forms.js"], nil)
	handler.ServeHTTP(versioned, versionedRequest)
	if versioned.Code != http.StatusOK {
		t.Fatalf("versioned asset status = %d, want 200", versioned.Code)
	}
	if got := versioned.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Fatalf("versioned Cache-Control = %q", got)
	}
	if got := versioned.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q", got)
	}

	unversioned := httptest.NewRecorder()
	unversionedRequest := httptest.NewRequest(http.MethodGet, "/public-assets/forms.js", nil)
	handler.ServeHTTP(unversioned, unversionedRequest)
	if got := unversioned.Header().Get("Cache-Control"); got != "public, max-age=0, must-revalidate" {
		t.Fatalf("unversioned Cache-Control = %q", got)
	}
}

func TestPublicTemplateUsesContentVersionedAssets(t *testing.T) {
	var rendered strings.Builder
	err := publicTemplate.ExecuteTemplate(&rendered, "public", publicView{
		Stage:     "landing",
		Title:     "فرم آزمایشی",
		Slug:      "sample",
		Theme:     "formal",
		Alignment: "start",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, asset := range []string{"forms.css", "forms.js"} {
		want := "/public-assets/" + asset + "?v=" + publicAssetVersions[asset]
		if !strings.Contains(rendered.String(), want) {
			t.Fatalf("rendered HTML does not contain %q", want)
		}
	}
}

func TestPublicTemplateExposesAccessibleCompositeFields(t *testing.T) {
	page := form.Page{ID: "page-one", Title: "صفحهٔ آزمون"}
	fields := []form.Field{
		{ID: "choice-field", Type: form.FieldYesNo, Label: "پاسخ", Required: true, HelpText: "یکی را انتخاب کنید."},
		{ID: "name-field", Type: form.FieldFullName, Label: "نام کامل", Required: true},
		{ID: "date-field", Type: form.FieldPersianDate, Label: "تاریخ", Required: true},
		{ID: "file-field", Type: form.FieldFile, Label: "پیوست", File: &form.FileRule{MaximumBytes: 1024, MaximumFiles: 1, Extensions: []string{"png"}}},
	}
	var rendered strings.Builder
	err := publicTemplate.ExecuteTemplate(&rendered, "public", publicView{
		Stage: "page", Title: "فرم آزمون", Slug: "sample", Theme: "formal", Alignment: "start",
		Page: &page, Fields: fields, CurrentStep: 1, TotalSteps: 2, ProgressPercent: 50,
		Errors: map[string]string{}, Values: map[string]string{}, Selections: map[string]map[string]bool{},
	})
	if err != nil {
		t.Fatal(err)
	}
	html := rendered.String()
	for _, want := range []string{
		`role="radiogroup" aria-labelledby="label-choice-field" aria-describedby="help-choice-field" aria-required="true"`,
		`role="group" aria-labelledby="label-name-field"`,
		`id="input-date-field" inputmode="numeric"`,
		`aria-describedby="note-date-field"`,
		`id="note-file-field"`,
		`صفحهٔ ۱ از ۲`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("rendered public page does not contain %q", want)
		}
	}
}
