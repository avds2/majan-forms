package web

import (
	"fmt"
	"html/template"
	"math"
	"net/http"
	"strconv"
	"strings"

	"forms/internal/persiandate"
	"forms/internal/response"
)

type printPoint struct {
	X, Y  string
	Label string
}

type printTrend struct {
	HasData                              bool
	ViewsPath, StartsPath, CompletesPath string
	Labels                               []printPoint
}

type printBar struct {
	Label, Count string
	Y, Width     string
}

type printReportData struct {
	Report     response.Report
	Trend      printTrend
	EndingBars []printBar
	Views      string
	Starts     string
	Completes  string
	Completion string
	Average    string
}

func (s *Server) printReport(w http.ResponseWriter, r *http.Request) {
	filter, err := reportFilterFromRequest(r)
	if err != nil {
		s.respondError(w, r, err)
		return
	}
	report, err := s.responses.Report(r.Context(), authenticatedUser(r), r.PathValue("projectID"), filter)
	if err != nil {
		s.respondError(w, r, err)
		return
	}
	data := printReportData{
		Report: report, Trend: buildPrintTrend(report.Trend, report.Segmented), EndingBars: buildPrintBars(report.Endings),
		Views:      persiandate.PersianDigits(strconv.FormatInt(report.KPI.Views, 10)),
		Starts:     persiandate.PersianDigits(strconv.FormatInt(report.KPI.Starts, 10)),
		Completes:  persiandate.PersianDigits(strconv.FormatInt(report.KPI.Completes, 10)),
		Completion: persiandate.PersianDigits(strconv.FormatFloat(report.KPI.CompletionRate, 'f', 1, 64)) + "٪",
		Average:    persiandate.PersianDigits(strconv.FormatFloat(report.KPI.AverageDurationSeconds, 'f', 1, 64)) + " ثانیه",
	}
	if report.Segmented {
		data.Completion = "—"
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Disposition", "inline")
	if err := printReportTemplate.Execute(w, data); err != nil {
		s.logger.ErrorContext(r.Context(), "render printable report", "error", err)
	}
}

func buildPrintTrend(points []response.TrendPoint, segmented bool) printTrend {
	if len(points) == 0 {
		return printTrend{}
	}
	maximum := int64(1)
	for _, point := range points {
		if segmented {
			maximum = max(maximum, point.Completes)
		} else {
			maximum = max(maximum, point.Views, point.Starts, point.Completes)
		}
	}
	path := func(value func(response.TrendPoint) int64) string {
		parts := make([]string, 0, len(points))
		for index, point := range points {
			x := 45.0
			if len(points) > 1 {
				x += float64(index) * 810 / float64(len(points)-1)
			}
			y := 220 - float64(value(point))*180/float64(maximum)
			command := "L"
			if index == 0 {
				command = "M"
			}
			parts = append(parts, fmt.Sprintf("%s%.1f %.1f", command, x, y))
		}
		return strings.Join(parts, " ")
	}
	labels := make([]printPoint, 0)
	step := max(1, int(math.Ceil(float64(len(points))/7)))
	for index, point := range points {
		if index%step != 0 && index != len(points)-1 {
			continue
		}
		x := 45.0
		if len(points) > 1 {
			x += float64(index) * 810 / float64(len(points)-1)
		}
		labels = append(labels, printPoint{X: fmt.Sprintf("%.1f", x), Y: "242", Label: point.DateJalali})
	}
	result := printTrend{
		HasData:       true,
		CompletesPath: path(func(point response.TrendPoint) int64 { return point.Completes }),
		Labels:        labels,
	}
	if !segmented {
		result.ViewsPath = path(func(point response.TrendPoint) int64 { return point.Views })
		result.StartsPath = path(func(point response.TrendPoint) int64 { return point.Starts })
	}
	return result
}

func buildPrintBars(values []response.CountBreakdown) []printBar {
	maximum := int64(0)
	for _, value := range values {
		maximum = max(maximum, value.Count)
	}
	if maximum == 0 {
		return nil
	}
	bars := make([]printBar, 0, len(values))
	for index, value := range values {
		versionLabel := "همهٔ نسخه‌ها"
		if !value.AcrossVersions {
			versionLabel = "نسخهٔ " + persiandate.PersianDigits(strconv.FormatInt(value.VersionNumber, 10))
		}
		bars = append(bars, printBar{
			Label: value.Label + " · " + versionLabel,
			Count: persiandate.PersianDigits(strconv.FormatInt(value.Count, 10)),
			Y:     fmt.Sprint(20 + index*38), Width: fmt.Sprintf("%.1f", float64(value.Count)*580/float64(maximum)),
		})
	}
	return bars
}

func addPrintText(value string, amount int) int {
	parsed, _ := strconv.Atoi(value)
	return parsed + amount
}

var printReportTemplate = template.Must(template.New("report").Funcs(template.FuncMap{
	"add":          func(a, b int) int { return a + b },
	"mul":          func(a, b int) int { return a * b },
	"barCount":     func(values []printBar) int { return len(values) },
	"addText":      addPrintText,
	"persianInt":   func(value int64) string { return persiandate.PersianDigits(strconv.FormatInt(value, 10)) },
	"persianFloat": func(value float64) string { return persiandate.PersianDigits(strconv.FormatFloat(value, 'f', 2, 64)) },
	"persianOptionalFloat": func(value *float64) string {
		if value == nil {
			return "—"
		}
		return persiandate.PersianDigits(strconv.FormatFloat(*value, 'f', 1, 64))
	},
}).Parse(printReportTemplateSource))

const printReportTemplateSource = `<!doctype html>
<html lang="fa" dir="rtl"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>گزارش {{.Report.ProjectTitle}}</title><link rel="stylesheet" href="/report-assets/report.css"><script defer src="/report-assets/report.js"></script></head>
<body><div class="toolbar"><button type="button" data-print-report>چاپ یا ذخیرهٔ PDF</button></div>
<main class="report"><header class="report-head"><div><small>گزارش فرم</small><h1>{{.Report.ProjectTitle}}</h1><p>ساخته‌شده در {{.Report.GeneratedJalali}}</p></div><div class="report-mark">ف</div></header>
<section class="kpis" aria-label="شاخص‌های اصلی"><div class="kpi"><span>بازدید</span><strong>{{.Views}}</strong></div><div class="kpi"><span>شروع</span><strong>{{.Starts}}</strong></div><div class="kpi"><span>تکمیل</span><strong>{{.Completes}}</strong></div><div class="kpi"><span>نرخ تکمیل</span><strong>{{.Completion}}</strong></div><div class="kpi"><span>میانگین زمان تکمیل</span><strong>{{.Average}}</strong></div></section>
{{if .Report.Segmented}}<section class="section"><p>فیلتر پایان یا پاسخ فقط روی پاسخ‌های تکمیل‌شده اعمال شده است؛ بازدید، شروع و فعالیت صفحه‌ها کل ترافیک بازه را نشان می‌دهند.</p></section>{{end}}
<section class="section"><h2>{{if .Report.Segmented}}روند تکمیل پاسخ‌های فیلترشده{{else}}روند زمانی{{end}}</h2>{{if .Trend.HasData}}<svg class="chart" viewBox="0 0 900 260" role="img" aria-label="نمودار روند تکمیل"><line class="axis" x1="45" y1="220" x2="855" y2="220"/><line class="grid" x1="45" y1="130" x2="855" y2="130"/><line class="grid" x1="45" y1="40" x2="855" y2="40"/>{{if not .Report.Segmented}}<path class="line-views" d="{{.Trend.ViewsPath}}"/><path class="line-starts" d="{{.Trend.StartsPath}}"/>{{end}}<path class="line-completes" d="{{.Trend.CompletesPath}}"/>{{range .Trend.Labels}}<text x="{{.X}}" y="{{.Y}}" text-anchor="middle">{{.Label}}</text>{{end}}</svg><div class="legend">{{if not .Report.Segmented}}<span><i class="views"></i>بازدید</span><span><i class="starts"></i>شروع</span>{{end}}<span><i class="completes"></i>تکمیل</span></div>{{else}}<div class="empty">هنوز داده‌ای برای این بازه وجود ندارد.</div>{{end}}</section>
<section class="section"><h2>پایان‌های فرم</h2>{{if .EndingBars}}<svg class="chart" viewBox="0 0 900 {{add 35 (mul (barCount .EndingBars) 38)}}" role="img" aria-label="نمودار پایان‌های فرم">{{range .EndingBars}}<text x="18" y="{{addText .Y 16}}">{{.Label}}</text><rect class="bar" x="280" y="{{.Y}}" width="{{.Width}}" height="22" rx="5"/><text x="870" y="{{addText .Y 16}}" text-anchor="end">{{.Count}}</text>{{end}}</svg>{{else}}<div class="empty">پاسخ تکمیل‌شده‌ای وجود ندارد.</div>{{end}}</section>
{{if .Report.Choices}}<section class="section"><h2>توزیع انتخاب‌ها</h2><table><thead><tr><th>پرسش</th><th>نسخه</th><th>گزینه</th><th>تعداد</th></tr></thead><tbody>{{range .Report.Choices}}{{$field := .}}{{range .Values}}<tr><td>{{$field.FieldLabel}}</td><td>{{if $field.AcrossVersions}}همهٔ نسخه‌ها{{else}}{{persianInt $field.VersionNumber}}{{end}}</td><td>{{.Label}}</td><td>{{persianInt .Count}}</td></tr>{{end}}{{end}}</tbody></table></section>{{end}}
{{if .Report.Numbers}}<section class="section"><h2>پاسخ‌های عددی</h2><table><thead><tr><th>پرسش</th><th>نسخه</th><th>تعداد</th><th>کمینه</th><th>میانه</th><th>میانگین</th><th>بیشینه</th><th>NPS</th></tr></thead><tbody>{{range .Report.Numbers}}<tr><td>{{.FieldLabel}}</td><td>{{if .AcrossVersions}}همهٔ نسخه‌ها{{else}}{{persianInt .VersionNumber}}{{end}}</td><td>{{persianInt .Count}}</td><td>{{persianFloat .Minimum}}</td><td>{{persianFloat .Median}}</td><td>{{persianFloat .Average}}</td><td>{{persianFloat .Maximum}}</td><td>{{persianOptionalFloat .NPS}}</td></tr>{{end}}</tbody></table></section>{{end}}
<footer class="report-foot">این گزارش با HTML، CSS چاپ و SVG ساخته شده و برای چاپ A4 به مرورگر دیگری در سرور نیاز ندارد.</footer></main></body></html>`
