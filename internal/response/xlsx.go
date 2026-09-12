package response

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"forms/internal/form"
	"forms/internal/persiandate"
)

type sheetColumnKind int

const (
	columnPublicID sheetColumnKind = iota
	columnVersion
	columnSubmittedJalali
	columnSubmittedISO
	columnEnding
	columnDuration
	columnStatus
	columnFieldVisible
	columnFieldRaw
)

type sheetColumn struct {
	Header  string
	Kind    sheetColumnKind
	FieldID string
	Variant string
	Hidden  bool
	Width   float64
}

type sheetCellKind int

const (
	cellBlank sheetCellKind = iota
	cellText
	cellNumber
	cellBoolean
	cellDate
	cellDateTime
	cellTime
)

type sheetCell struct {
	Kind    sheetCellKind
	Text    string
	Boolean bool
	Time    time.Time
}

func (e *PreparedExport) writeXLSX(ctx context.Context, destination io.Writer) error {
	archive := zip.NewWriter(destination)
	staticFiles := []struct {
		name, contents string
	}{
		{"[Content_Types].xml", xlsxContentTypes},
		{"_rels/.rels", xlsxRootRelationships},
		{"xl/workbook.xml", xlsxWorkbook},
		{"xl/_rels/workbook.xml.rels", xlsxWorkbookRelationships},
		{"xl/styles.xml", xlsxStyles},
	}
	for _, file := range staticFiles {
		if err := writeZIPFile(archive, file.name, func(writer io.Writer) error {
			_, err := io.WriteString(writer, file.contents)
			return err
		}); err != nil {
			archive.Close() //nolint:errcheck
			return fmt.Errorf("write XLSX part %s: %w", file.name, err)
		}
	}
	columns := e.sheetColumns()
	err := writeZIPFile(archive, "xl/worksheets/sheet1.xml", func(writer io.Writer) error {
		return e.writeSheet(ctx, writer, columns)
	})
	if err != nil {
		archive.Close() //nolint:errcheck
		return fmt.Errorf("write XLSX worksheet: %w", err)
	}
	if err := archive.Close(); err != nil {
		return fmt.Errorf("finish XLSX export: %w", err)
	}
	return nil
}

func writeZIPFile(archive *zip.Writer, name string, write func(io.Writer) error) error {
	header := &zip.FileHeader{Name: name, Method: zip.Deflate}
	header.SetModTime(time.Unix(0, 0).UTC())
	writer, err := archive.CreateHeader(header)
	if err != nil {
		return err
	}
	return write(writer)
}

func (e *PreparedExport) sheetColumns() []sheetColumn {
	columns := []sheetColumn{
		{Header: "شناسهٔ پاسخ", Kind: columnPublicID, Width: 38},
		{Header: "نسخه", Kind: columnVersion, Width: 10},
		{Header: "زمان ثبت (شمسی)", Kind: columnSubmittedJalali, Width: 24},
		{Header: "زمان ثبت (ISO)", Kind: columnSubmittedISO, Hidden: true, Width: 22},
		{Header: "پایان", Kind: columnEnding, Width: 24},
		{Header: "مدت تکمیل (ثانیه)", Kind: columnDuration, Width: 18},
		{Header: "وضعیت", Kind: columnStatus, Width: 14},
	}
	for _, field := range e.fields {
		header := uniqueFieldHeader(field, e.fields)
		columns = append(columns, sheetColumn{Header: header, Kind: columnFieldVisible, FieldID: field.ID, Width: 28})
		switch field.Type {
		case form.FieldYesNo, form.FieldConsent:
			columns = append(columns, sheetColumn{Header: header + " — مقدار فنی", Kind: columnFieldRaw, FieldID: field.ID, Hidden: true, Width: 14})
		case form.FieldPersianDate:
			columns = append(columns, sheetColumn{Header: header + " — تاریخ ISO", Kind: columnFieldRaw, FieldID: field.ID, Hidden: true, Width: 16})
		case form.FieldDateTime:
			columns = append(columns, sheetColumn{Header: header + " — زمان ISO", Kind: columnFieldRaw, FieldID: field.ID, Hidden: true, Width: 22})
		case form.FieldTime:
			columns = append(columns, sheetColumn{Header: header + " — زمان فنی", Kind: columnFieldRaw, FieldID: field.ID, Hidden: true, Width: 14})
		case form.FieldDateRange:
			columns = append(columns,
				sheetColumn{Header: header + " — آغاز ISO", Kind: columnFieldRaw, FieldID: field.ID, Variant: "from", Hidden: true, Width: 16},
				sheetColumn{Header: header + " — پایان ISO", Kind: columnFieldRaw, FieldID: field.ID, Variant: "to", Hidden: true, Width: 16},
			)
		}
	}
	return columns
}

func (e *PreparedExport) writeSheet(ctx context.Context, writer io.Writer, columns []sheetColumn) error {
	lastCell := cellReference(len(columns), int(e.count)+1)
	if _, err := io.WriteString(writer, `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`+
		`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">`+
		`<dimension ref="A1:`+lastCell+`"/><sheetViews><sheetView rightToLeft="1" workbookViewId="0">`+
		`<pane ySplit="1" topLeftCell="A2" activePane="bottomLeft" state="frozen"/>`+
		`</sheetView></sheetViews><sheetFormatPr defaultRowHeight="20"/><cols>`); err != nil {
		return err
	}
	for index, column := range columns {
		hidden := ""
		if column.Hidden {
			hidden = ` hidden="1"`
		}
		if _, err := fmt.Fprintf(writer, `<col min="%d" max="%d" width="%s" customWidth="1"%s/>`, index+1, index+1, strconv.FormatFloat(column.Width, 'f', 1, 64), hidden); err != nil {
			return err
		}
	}
	if _, err := io.WriteString(writer, `</cols><sheetData><row r="1" ht="30" customHeight="1">`); err != nil {
		return err
	}
	for index, column := range columns {
		if err := writeSheetCell(writer, cellReference(index+1, 1), sheetCell{Kind: cellText, Text: column.Header}, true); err != nil {
			return err
		}
	}
	if _, err := io.WriteString(writer, `</row>`); err != nil {
		return err
	}
	rowNumber := 2
	err := e.service.store.StreamExport(ctx, e.projectID, e.filter, func(record exportRecord) error {
		if _, err := fmt.Fprintf(writer, `<row r="%d">`, rowNumber); err != nil {
			return err
		}
		for columnIndex, column := range columns {
			cell := e.sheetCell(record, column)
			if err := writeSheetCell(writer, cellReference(columnIndex+1, rowNumber), cell, false); err != nil {
				return err
			}
		}
		if _, err := io.WriteString(writer, `</row>`); err != nil {
			return err
		}
		rowNumber++
		return nil
	})
	if err != nil {
		return err
	}
	filterEnd := cellReference(len(columns), int(e.count)+1)
	_, err = io.WriteString(writer, `</sheetData><autoFilter ref="A1:`+filterEnd+`"/>`+
		`<pageMargins left="0.25" right="0.25" top="0.5" bottom="0.5" header="0.2" footer="0.2"/>`+
		`<pageSetup orientation="landscape" fitToWidth="1" fitToHeight="0"/>`+
		`</worksheet>`)
	return err
}

func (e *PreparedExport) sheetCell(record exportRecord, column sheetColumn) sheetCell {
	version := e.versionMap[record.VersionID]
	switch column.Kind {
	case columnPublicID:
		return sheetCell{Kind: cellText, Text: record.PublicID}
	case columnVersion:
		return sheetCell{Kind: cellNumber, Text: strconv.FormatInt(record.VersionNumber, 10)}
	case columnSubmittedJalali:
		value, _ := persiandate.FormatTime(record.SubmittedAt, e.service.location)
		return sheetCell{Kind: cellText, Text: value}
	case columnSubmittedISO:
		return sheetCell{Kind: cellDateTime, Time: record.SubmittedAt.UTC()}
	case columnEnding:
		return sheetCell{Kind: cellText, Text: endingLabel(version.Schema, record.EndingID)}
	case columnDuration:
		return sheetCell{Kind: cellNumber, Text: strconv.FormatInt(record.DurationSeconds, 10)}
	case columnStatus:
		if record.DeletedAt != nil {
			return sheetCell{Kind: cellText, Text: "حذف‌شده"}
		}
		return sheetCell{Kind: cellText, Text: "فعال"}
	case columnFieldVisible:
		return e.visibleFieldCell(record, column.FieldID)
	case columnFieldRaw:
		return e.rawFieldCell(record, column.FieldID, column.Variant)
	default:
		return sheetCell{}
	}
}

func (e *PreparedExport) visibleFieldCell(record exportRecord, fieldID string) sheetCell {
	if files := record.Files[fieldID]; len(files) > 0 {
		names := make([]string, 0, len(files))
		for _, file := range files {
			names = append(names, file.Name)
		}
		return sheetCell{Kind: cellText, Text: strings.Join(names, " | ")}
	}
	answer, exists := record.Answers[fieldID]
	if !exists {
		return sheetCell{}
	}
	version := e.versionMap[record.VersionID]
	field, exists := schemaField(version.Schema, fieldID)
	if !exists {
		return sheetCell{}
	}
	if isNumericType(answer.Type) {
		switch number := answer.Value.(type) {
		case json.Number:
			if _, err := number.Float64(); err == nil {
				return sheetCell{Kind: cellNumber, Text: number.String()}
			}
		case float64:
			return sheetCell{Kind: cellNumber, Text: strconv.FormatFloat(number, 'f', -1, 64)}
		}
	}
	return sheetCell{Kind: cellText, Text: displayValue(field, answer.Value, e.service.location)}
}

func (e *PreparedExport) rawFieldCell(record exportRecord, fieldID, variant string) sheetCell {
	answer, exists := record.Answers[fieldID]
	if !exists {
		return sheetCell{}
	}
	switch answer.Type {
	case form.FieldYesNo, form.FieldConsent:
		if value, ok := answer.Value.(bool); ok {
			return sheetCell{Kind: cellBoolean, Boolean: value}
		}
	case form.FieldPersianDate:
		if parsed, err := time.Parse(time.DateOnly, fmt.Sprint(answer.Value)); err == nil {
			return sheetCell{Kind: cellDate, Time: parsed}
		}
	case form.FieldDateTime:
		if parsed, err := time.Parse(time.RFC3339, fmt.Sprint(answer.Value)); err == nil {
			return sheetCell{Kind: cellDateTime, Time: parsed.UTC()}
		}
	case form.FieldTime:
		if parsed, ok := parseClock(fmt.Sprint(answer.Value)); ok {
			return sheetCell{Kind: cellTime, Time: parsed}
		}
	case form.FieldDateRange:
		if values, ok := answer.Value.(map[string]any); ok {
			if parsed, err := time.Parse(time.DateOnly, fmt.Sprint(values[variant])); err == nil {
				return sheetCell{Kind: cellDate, Time: parsed}
			}
		}
	}
	return sheetCell{}
}

func parseClock(value string) (time.Time, bool) {
	for _, layout := range []string{"15:04:05.999999999", "15:04:05", "15:04"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

func writeSheetCell(writer io.Writer, reference string, cell sheetCell, header bool) error {
	if cell.Kind == cellBlank {
		return nil
	}
	style := 5
	if header {
		style = 4
	}
	switch cell.Kind {
	case cellText:
		_, err := io.WriteString(writer, `<c r="`+reference+`" t="inlineStr" s="`+strconv.Itoa(style)+`"><is><t xml:space="preserve">`+escapeXML(cell.Text)+`</t></is></c>`)
		return err
	case cellNumber:
		_, err := io.WriteString(writer, `<c r="`+reference+`"><v>`+escapeXML(cell.Text)+`</v></c>`)
		return err
	case cellBoolean:
		value := "0"
		if cell.Boolean {
			value = "1"
		}
		_, err := io.WriteString(writer, `<c r="`+reference+`" t="b"><v>`+value+`</v></c>`)
		return err
	case cellDate:
		_, err := io.WriteString(writer, `<c r="`+reference+`" s="1"><v>`+excelSerial(cell.Time, false)+`</v></c>`)
		return err
	case cellDateTime:
		_, err := io.WriteString(writer, `<c r="`+reference+`" s="2"><v>`+excelSerial(cell.Time, true)+`</v></c>`)
		return err
	case cellTime:
		seconds := float64(cell.Time.Hour()*3600+cell.Time.Minute()*60+cell.Time.Second()) + float64(cell.Time.Nanosecond())/1e9
		_, err := io.WriteString(writer, `<c r="`+reference+`" s="3"><v>`+strconv.FormatFloat(seconds/86400, 'f', 12, 64)+`</v></c>`)
		return err
	default:
		return nil
	}
}

func excelSerial(value time.Time, includeTime bool) string {
	value = value.UTC()
	if !includeTime {
		value = time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
	}
	epoch := time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)
	return strconv.FormatFloat(value.Sub(epoch).Hours()/24, 'f', 10, 64)
}

func cellReference(column, row int) string {
	if column < 1 {
		column = 1
	}
	letters := ""
	for column > 0 {
		column--
		letters = string(rune('A'+column%26)) + letters
		column /= 26
	}
	return letters + strconv.Itoa(row)
}

func escapeXML(value string) string {
	var buffer bytes.Buffer
	_ = xml.EscapeText(&buffer, []byte(value))
	return buffer.String()
}

const xlsxContentTypes = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
  <Default Extension="xml" ContentType="application/xml"/>
  <Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>
  <Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>
  <Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>
</Types>`

const xlsxRootRelationships = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/>
</Relationships>`

const xlsxWorkbook = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
  <workbookPr date1904="0"/><bookViews><workbookView activeTab="0"/></bookViews>
  <sheets><sheet name="پاسخ‌ها" sheetId="1" r:id="rId1"/></sheets>
</workbook>`

const xlsxWorkbookRelationships = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/>
  <Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>
</Relationships>`

const xlsxStyles = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <numFmts count="2"><numFmt numFmtId="164" formatCode="yyyy-mm-dd hh:mm:ss"/><numFmt numFmtId="165" formatCode="hh:mm:ss"/></numFmts>
  <fonts count="2"><font><sz val="11"/><name val="Tahoma"/></font><font><b/><color rgb="FFFFFFFF"/><sz val="11"/><name val="Tahoma"/></font></fonts>
  <fills count="2"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="solid"><fgColor rgb="FF0F766E"/><bgColor indexed="64"/></patternFill></fill></fills>
  <borders count="2"><border/><border><left style="thin"><color rgb="FFDCE4EB"/></left><right style="thin"><color rgb="FFDCE4EB"/></right><top style="thin"><color rgb="FFDCE4EB"/></top><bottom style="thin"><color rgb="FFDCE4EB"/></bottom></border></borders>
  <cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs>
  <cellXfs count="6">
    <xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/>
    <xf numFmtId="14" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/>
    <xf numFmtId="164" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/>
    <xf numFmtId="165" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/>
    <xf numFmtId="0" fontId="1" fillId="1" borderId="1" xfId="0" applyAlignment="1"><alignment horizontal="right" vertical="center" wrapText="1"/></xf>
    <xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0" applyAlignment="1"><alignment horizontal="right" vertical="top" wrapText="1"/></xf>
  </cellXfs>
  <cellStyles count="1"><cellStyle name="Normal" xfId="0" builtinId="0"/></cellStyles>
</styleSheet>`
