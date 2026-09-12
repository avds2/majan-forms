package form

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"forms/internal/normalize"
)

type Evaluation struct {
	PageIDs  []string       `json:"page_ids"`
	EndingID string         `json:"ending_id"`
	Answers  map[string]any `json:"answers"`
}

func (s Schema) ValidateForPublish() error {
	if err := s.Validate(); err != nil {
		return err
	}
	problems := make([]ValidationProblem, 0)
	add := func(path, message string) {
		problems = append(problems, ValidationProblem{Path: path, Message: message})
	}
	pages := make(map[string]Page, len(s.Pages))
	endings := make(map[string]Ending, len(s.Endings))
	fields := make(map[string]Field)
	fieldPage := make(map[string]int)
	fieldPageID := make(map[string]string)
	for pageIndex, page := range s.Pages {
		pages[page.ID] = page
		for _, field := range page.Fields {
			fields[field.ID] = field
			fieldPage[field.ID] = pageIndex
			fieldPageID[field.ID] = page.ID
		}
	}
	for _, ending := range s.Endings {
		endings[ending.ID] = ending
	}
	if _, ok := pages[s.Flow.StartPageID]; !ok {
		add("flow.start_page_id", "صفحهٔ شروع پیدا نشد")
	}
	edgesBySource := make(map[string][]FlowEdge)
	for index, edge := range s.Flow.Edges {
		path := fmt.Sprintf("flow.edges[%d]", index)
		if _, ok := pages[edge.SourceID]; !ok {
			add(path+".source_id", "صفحهٔ مبدأ پیدا نشد")
		}
		if _, pageTarget := pages[edge.TargetID]; !pageTarget {
			if _, endingTarget := endings[edge.TargetID]; !endingTarget {
				add(path+".target_id", "مقصد مسیر پیدا نشد")
			}
		}
		if edge.SourceID == edge.TargetID {
			add(path+".target_id", "مسیر نمی‌تواند به همان صفحه برگردد")
		}
		if edge.Fallback && edge.Condition != nil {
			add(path+".condition", "مسیر پیش‌فرض نباید شرط داشته باشد")
		}
		if !edge.Fallback && edge.Condition == nil {
			add(path+".condition", "مسیر شرطی باید شرط داشته باشد")
		}
		if edge.Condition != nil {
			validateConditionReferences(*edge.Condition, path+".condition", edge.SourceID, fields, fieldPageID, s.Flow.Edges, add)
		}
		for actionIndex, action := range edge.Actions {
			validateAssignment(action, fmt.Sprintf("%s.actions[%d]", path, actionIndex), edge.SourceID, edge.TargetID, fields, fieldPage, fieldPageID, s.Pages, s.Flow.Edges, add)
		}
		edgesBySource[edge.SourceID] = append(edgesBySource[edge.SourceID], edge)
	}
	for pageIndex, page := range s.Pages {
		edges := edgesBySource[page.ID]
		fallbacks := 0
		priorities := make(map[int]bool)
		for _, edge := range edges {
			if edge.Fallback {
				fallbacks++
			}
			if priorities[edge.Priority] {
				add(fmt.Sprintf("pages[%d].flow", pageIndex), "اولویت مسیرها باید یکتا باشد")
			}
			priorities[edge.Priority] = true
		}
		if fallbacks != 1 {
			add(fmt.Sprintf("pages[%d].flow", pageIndex), "هر صفحه باید دقیقاً یک مسیر پیش‌فرض داشته باشد")
		}
	}
	validateAcyclicAndReachable(s, pages, endings, edgesBySource, add)
	if len(problems) > 0 {
		return &SchemaError{Problems: problems}
	}
	return nil
}

func validateAcyclicAndReachable(schema Schema, pages map[string]Page, endings map[string]Ending, edges map[string][]FlowEdge, add func(string, string)) {
	state := make(map[string]uint8)
	var visit func(string) bool
	visit = func(node string) bool {
		if state[node] == 1 {
			return false
		}
		if state[node] == 2 {
			return true
		}
		state[node] = 1
		for _, edge := range edges[node] {
			if _, isPage := pages[edge.TargetID]; isPage && !visit(edge.TargetID) {
				return false
			}
		}
		state[node] = 2
		return true
	}
	for pageID := range pages {
		if state[pageID] == 0 && !visit(pageID) {
			add("flow.edges", "گراف مسیر دارای چرخه است")
			break
		}
	}
	reachable := make(map[string]bool)
	queue := []string{schema.Flow.StartPageID}
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		if reachable[node] {
			continue
		}
		reachable[node] = true
		for _, edge := range edges[node] {
			queue = append(queue, edge.TargetID)
		}
	}
	for pageID := range pages {
		if !reachable[pageID] {
			add("flow.edges", "حداقل یک صفحه از مسیر شروع قابل‌دسترسی نیست")
			break
		}
	}
	endingReachable := false
	for endingID := range endings {
		if reachable[endingID] {
			endingReachable = true
		} else {
			add("flow.edges", "حداقل یک پایان از مسیر شروع قابل‌دسترسی نیست")
			break
		}
	}
	if !endingReachable {
		add("flow.edges", "هیچ پایان قابل‌دسترسی وجود ندارد")
	}
}

func validateConditionReferences(group ConditionGroup, path, sourcePageID string, fields map[string]Field, fieldPage map[string]string, edges []FlowEdge, add func(string, string)) {
	for index, condition := range group.Conditions {
		conditionPath := fmt.Sprintf("%s.conditions[%d]", path, index)
		field, ok := fields[condition.FieldID]
		if !ok {
			add(conditionPath+".field_id", "فیلد شرط پیدا نشد")
			continue
		}
		if field.Type != FieldHidden && field.Type != FieldCalculated && !pageCanReach(edges, fieldPage[field.ID], sourcePageID) {
			add(conditionPath+".field_id", "فیلد شرط باید در همین صفحه یا یکی از صفحه‌های پیشین مسیر باشد")
			continue
		}
		if !operatorAllowed(field.Type, condition.Operator) {
			add(conditionPath+".operator", "عملگر با نوع فیلد سازگار نیست")
			continue
		}
		if !conditionValueValid(field, condition.Operator, condition.Value) {
			add(conditionPath+".value", "مقدار شرط با نوع فیلد سازگار نیست")
		}
	}
	for index, child := range group.Groups {
		validateConditionReferences(child, fmt.Sprintf("%s.groups[%d]", path, index), sourcePageID, fields, fieldPage, edges, add)
	}
}

func validateAssignment(action AssignmentAction, path, sourcePageID, edgeTargetID string, fields map[string]Field, fieldPage map[string]int, fieldPageID map[string]string, pages []Page, edges []FlowEdge, add func(string, string)) {
	target, ok := fields[action.TargetFieldID]
	if !ok || isDisplayField(target.Type) || target.Type == FieldFile || target.Type == FieldSignature {
		add(path+".target_field_id", "فیلد مقصد تغییر مقدار معتبر نیست")
		return
	}
	sourcePageIndex := -1
	for index, page := range pages {
		if page.ID == sourcePageID {
			sourcePageIndex = index
			break
		}
	}
	if target.Type != FieldHidden && target.Type != FieldCalculated &&
		(fieldPage[target.ID] <= sourcePageIndex || !pageCanReach(edges, edgeTargetID, fieldPageID[target.ID])) {
		add(path+".target_field_id", "مقصد باید فیلد مخفی/محاسباتی یا فیلدی در ادامهٔ همین مسیر باشد")
	}
	if action.Source == "copy" {
		source, ok := fields[action.SourceFieldID]
		if !ok || !compatibleTypes(source.Type, target.Type) {
			add(path+".source_field_id", "فیلد منبع با مقصد سازگار نیست")
		} else if source.Type != FieldHidden && source.Type != FieldCalculated && !pageCanReach(edges, fieldPageID[source.ID], sourcePageID) {
			add(path+".source_field_id", "فیلد منبع باید در همین صفحه یا یکی از صفحه‌های پیشین مسیر باشد")
		}
	} else if action.Source == "constant" && !answerValueValid(target, action.Value) {
		add(path+".value", "مقدار ثابت با فیلد مقصد سازگار نیست")
	}
}

func pageCanReach(edges []FlowEdge, from, target string) bool {
	if from == "" || target == "" {
		return false
	}
	queue := []string{from}
	seen := make(map[string]bool)
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		if node == target {
			return true
		}
		if seen[node] {
			continue
		}
		seen[node] = true
		for _, edge := range edges {
			if edge.SourceID == node {
				queue = append(queue, edge.TargetID)
			}
		}
	}
	return false
}

func operatorAllowed(fieldType FieldType, operator string) bool {
	common := operator == "empty" || operator == "not_empty"
	if common {
		return !isDisplayField(fieldType)
	}
	switch {
	case isNumberField(fieldType):
		return operator == "equals" || operator == "not_equals" || operator == "greater_than" || operator == "less_than" || operator == "between"
	case fieldType == FieldDateRange || fieldType == FieldFullName || fieldType == FieldAddress:
		return false
	case fieldType == FieldPersianDate || fieldType == FieldDateTime || fieldType == FieldTime:
		return operator == "equals" || operator == "not_equals" || operator == "before" || operator == "after" || operator == "between"
	case fieldType == FieldMultiChoice || fieldType == FieldMultiDropdown || fieldType == FieldMatrix:
		return operator == "contains_any" || operator == "contains_all"
	case fieldType == FieldYesNo || fieldType == FieldConsent || fieldType == FieldSingleChoice || fieldType == FieldDropdown:
		return operator == "equals" || operator == "not_equals"
	case !isDisplayField(fieldType) && fieldType != FieldFile && fieldType != FieldSignature:
		return operator == "equals" || operator == "not_equals" || operator == "contains" || operator == "not_contains"
	default:
		return false
	}
}

func conditionValueValid(field Field, operator string, value any) bool {
	if operator == "empty" || operator == "not_empty" {
		return value == nil
	}
	if operator == "between" {
		values, ok := toSlice(value)
		if !ok || len(values) != 2 || !answerValueValid(field, values[0]) || !answerValueValid(field, values[1]) {
			return false
		}
		if minimum, minOK := toFloat(values[0]); minOK {
			maximum, maxOK := toFloat(values[1])
			return maxOK && minimum <= maximum
		}
		minimum, minOK := comparableTime(values[0])
		maximum, maxOK := comparableTime(values[1])
		return minOK && maxOK && !minimum.After(maximum)
	}
	if operator == "contains_any" || operator == "contains_all" {
		values, ok := toSlice(value)
		if !ok || len(values) == 0 {
			return false
		}
		for _, item := range values {
			if !collectionItemValid(field, item) {
				return false
			}
		}
		return true
	}
	return answerValueValid(field, value)
}

func answerValueValid(field Field, value any) bool {
	if value == nil {
		return false
	}
	if isNumberField(field.Type) {
		number, ok := toFloat(value)
		if !ok {
			return false
		}
		if field.Number != nil {
			if field.Number.Minimum != nil && number < *field.Number.Minimum || field.Number.Maximum != nil && number > *field.Number.Maximum {
				return false
			}
			if field.Number.Integer && number != math.Trunc(number) {
				return false
			}
		}
		if field.Type == FieldPercent && (number < 0 || number > 100) {
			return false
		}
		if field.Type == FieldRating || field.Type == FieldScale || field.Type == FieldNPS {
			minimum, maximum := 1, 5
			if field.Type == FieldNPS {
				minimum, maximum = 0, 10
			}
			if field.Scale != nil {
				minimum, maximum = field.Scale.Minimum, field.Scale.Maximum
			}
			return number == math.Trunc(number) && number >= float64(minimum) && number <= float64(maximum)
		}
		return true
	}
	if field.Type == FieldPersianDate {
		text, ok := value.(string)
		if !ok {
			return false
		}
		_, err := time.Parse(time.DateOnly, text)
		return err == nil && dateValueWithinRule(field.Date, text)
	}
	if field.Type == FieldDateTime {
		text, ok := value.(string)
		if !ok {
			return false
		}
		parsed, err := time.Parse(time.RFC3339, text)
		return err == nil && dateValueWithinRule(field.Date, parsed.Format(time.DateOnly))
	}
	if field.Type == FieldTime {
		text, ok := value.(string)
		if !ok {
			return false
		}
		_, err := time.Parse("15:04", text)
		return err == nil
	}
	if field.Type == FieldYesNo || field.Type == FieldConsent {
		_, ok := value.(bool)
		return ok
	}
	if field.Type == FieldSingleChoice || field.Type == FieldDropdown {
		return collectionItemValid(field, value)
	}
	if field.Type == FieldMultiChoice || field.Type == FieldMultiDropdown {
		values, ok := toSlice(value)
		if !ok || len(values) == 0 {
			return false
		}
		seen := make(map[string]bool, len(values))
		for _, item := range values {
			if !collectionItemValid(field, item) {
				return false
			}
			key := fmt.Sprint(item)
			if seen[key] {
				return false
			}
			seen[key] = true
		}
		return true
	}
	if field.Type == FieldMatrix {
		return matrixAnswerValid(field, value)
	}
	if field.Type == FieldFullName {
		parts, ok := value.(map[string]any)
		if !ok {
			return false
		}
		first, firstOK := parts["first"].(string)
		last, lastOK := parts["last"].(string)
		return firstOK && lastOK && strings.TrimSpace(first) != "" && strings.TrimSpace(last) != ""
	}
	if field.Type == FieldAddress {
		parts, ok := value.(map[string]any)
		if !ok {
			return false
		}
		address, ok := parts["address"].(string)
		return ok && strings.TrimSpace(address) != ""
	}
	text, ok := value.(string)
	if !ok || len([]rune(text)) > 100_000 {
		return false
	}
	if field.Text != nil {
		length := len([]rune(text))
		if length < field.Text.MinimumLength || field.Text.MaximumLength > 0 && length > field.Text.MaximumLength {
			return false
		}
		if field.Text.Pattern != "" {
			pattern, err := regexp.Compile(field.Text.Pattern)
			if err != nil || !pattern.MatchString(text) {
				return false
			}
		}
	}
	if field.Type == FieldURL && !safeHTTPSURL(text) {
		return false
	}
	return true
}

func dateValueWithinRule(rule *DateRule, value string) bool {
	return rule == nil || (rule.Minimum == "" || value >= rule.Minimum) && (rule.Maximum == "" || value <= rule.Maximum)
}

func collectionItemValid(field Field, value any) bool {
	text, ok := value.(string)
	if !ok {
		return false
	}
	options := field.Options
	if field.Type == FieldMatrix && field.Matrix != nil {
		options = field.Matrix.Columns
	}
	for _, option := range options {
		if option.ID == text {
			return true
		}
	}
	return false
}

func matrixAnswerValid(field Field, value any) bool {
	if field.Matrix == nil {
		return false
	}
	rows, ok := value.(map[string]any)
	if !ok || len(rows) == 0 {
		return false
	}
	knownRows := make(map[string]bool, len(field.Matrix.Rows))
	for _, row := range field.Matrix.Rows {
		knownRows[row.ID] = true
	}
	for rowID, selected := range rows {
		if !knownRows[rowID] {
			return false
		}
		if field.Matrix.Multiple {
			items, ok := toSlice(selected)
			if !ok || len(items) == 0 {
				return false
			}
			for _, item := range items {
				if !collectionItemValid(field, item) {
					return false
				}
			}
		} else if !collectionItemValid(field, selected) {
			return false
		}
	}
	return !field.Required || len(rows) == len(field.Matrix.Rows)
}

func Evaluate(schema Schema, original map[string]any) (Evaluation, error) {
	if err := schema.ValidateForPublish(); err != nil {
		return Evaluation{}, err
	}
	answers := cloneAnswers(original)
	pages := make(map[string]bool, len(schema.Pages))
	endings := make(map[string]bool, len(schema.Endings))
	for _, page := range schema.Pages {
		pages[page.ID] = true
	}
	for _, ending := range schema.Endings {
		endings[ending.ID] = true
	}
	result := Evaluation{Answers: answers}
	current := schema.Flow.StartPageID
	for range len(schema.Pages) + 1 {
		result.PageIDs = append(result.PageIDs, current)
		edge, err := NextEdge(schema, current, answers)
		if err != nil {
			return Evaluation{}, err
		}
		applyAssignments(schema, edge.Actions, answers)
		if endings[edge.TargetID] {
			result.EndingID = edge.TargetID
			return result, nil
		}
		if !pages[edge.TargetID] {
			return Evaluation{}, fmt.Errorf("flow target %s is missing", edge.TargetID)
		}
		current = edge.TargetID
	}
	return Evaluation{}, fmt.Errorf("flow evaluation exceeded page limit")
}

func NextEdge(schema Schema, pageID string, answers map[string]any) (FlowEdge, error) {
	edges := make([]FlowEdge, 0)
	for _, edge := range schema.Flow.Edges {
		if edge.SourceID == pageID {
			edges = append(edges, edge)
		}
	}
	sort.SliceStable(edges, func(i, j int) bool { return edges[i].Priority < edges[j].Priority })
	var fallback *FlowEdge
	for index := range edges {
		edge := &edges[index]
		if edge.Fallback {
			fallback = edge
			continue
		}
		if edge.Condition != nil && evaluateGroup(*edge.Condition, answers) {
			return *edge, nil
		}
	}
	if fallback != nil {
		return *fallback, nil
	}
	return FlowEdge{}, fmt.Errorf("page %s has no matching or fallback edge", pageID)
}

func evaluateGroup(group ConditionGroup, answers map[string]any) bool {
	values := make([]bool, 0, len(group.Conditions)+len(group.Groups))
	for _, condition := range group.Conditions {
		values = append(values, evaluateCondition(condition, answers))
	}
	for _, child := range group.Groups {
		values = append(values, evaluateGroup(child, answers))
	}
	if group.Match == "any" {
		for _, value := range values {
			if value {
				return true
			}
		}
		return false
	}
	for _, value := range values {
		if !value {
			return false
		}
	}
	return len(values) > 0
}

func evaluateCondition(condition Condition, answers map[string]any) bool {
	actual, known := answers[condition.FieldID]
	if condition.Operator == "empty" {
		return !known || isEmpty(actual)
	}
	if condition.Operator == "not_empty" {
		return known && !isEmpty(actual)
	}
	if !known {
		return false
	}
	switch condition.Operator {
	case "contains_any", "contains_all":
		actualValues, actualOK := collectionValues(actual)
		expectedValues, expectedOK := toSlice(condition.Value)
		if !actualOK || !expectedOK {
			return false
		}
		matches := 0
		for _, expected := range expectedValues {
			for _, candidate := range actualValues {
				if normalizedEqual(candidate, expected) {
					matches++
					break
				}
			}
		}
		if condition.Operator == "contains_all" {
			return matches == len(expectedValues)
		}
		return matches > 0
	case "greater_than", "less_than":
		left, leftOK := toFloat(actual)
		right, rightOK := toFloat(condition.Value)
		if !leftOK || !rightOK {
			return false
		}
		if condition.Operator == "greater_than" {
			return left > right
		}
		return left < right
	case "before", "after":
		left, leftOK := comparableTime(actual)
		right, rightOK := comparableTime(condition.Value)
		if !leftOK || !rightOK {
			return false
		}
		if condition.Operator == "before" {
			return left.Before(right)
		}
		return left.After(right)
	case "between":
		bounds, ok := toSlice(condition.Value)
		if !ok || len(bounds) != 2 {
			return false
		}
		if number, numberOK := toFloat(actual); numberOK {
			minimum, minOK := toFloat(bounds[0])
			maximum, maxOK := toFloat(bounds[1])
			return minOK && maxOK && number >= minimum && number <= maximum
		}
		date, dateOK := comparableTime(actual)
		minimum, minOK := comparableTime(bounds[0])
		maximum, maxOK := comparableTime(bounds[1])
		return dateOK && minOK && maxOK && !date.Before(minimum) && !date.After(maximum)
	case "equals":
		return normalizedEqual(actual, condition.Value)
	case "not_equals":
		return !normalizedEqual(actual, condition.Value)
	case "contains", "not_contains":
		left, leftOK := actual.(string)
		right, rightOK := condition.Value.(string)
		if !leftOK || !rightOK {
			return false
		}
		contains := strings.Contains(strings.ToLower(normalize.PersianText(left)), strings.ToLower(normalize.PersianText(right)))
		if condition.Operator == "not_contains" {
			return !contains
		}
		return contains
	default:
		return false
	}
}

func applyAssignments(schema Schema, actions []AssignmentAction, answers map[string]any) {
	for _, action := range actions {
		if action.Mode == "if_empty" {
			if existing, ok := answers[action.TargetFieldID]; ok && !isEmpty(existing) {
				continue
			}
		}
		if action.Source == "copy" {
			if value, ok := answers[action.SourceFieldID]; ok {
				if target, found := schemaFieldByID(schema, action.TargetFieldID); found && answerValueValid(target, value) {
					answers[action.TargetFieldID] = value
				}
			}
			continue
		}
		if target, found := schemaFieldByID(schema, action.TargetFieldID); found && answerValueValid(target, action.Value) {
			answers[action.TargetFieldID] = action.Value
		}
	}
}

func schemaFieldByID(schema Schema, fieldID string) (Field, bool) {
	for _, page := range schema.Pages {
		for _, field := range page.Fields {
			if field.ID == fieldID {
				return field, true
			}
		}
	}
	return Field{}, false
}

// ApplyAssignments applies the already-validated, bounded assignment actions of
// one selected edge. Public navigation calls it only after the current page is
// accepted so future-page actions never run early.
func ApplyAssignments(schema Schema, actions []AssignmentAction, answers map[string]any) {
	applyAssignments(schema, actions, answers)
}

func compatibleTypes(left, right FieldType) bool {
	return left == right
}

func isTextField(fieldType FieldType) bool {
	switch fieldType {
	case FieldShortText, FieldLongText, FieldEmail, FieldPhone, FieldURL, FieldNationalID, FieldHidden:
		return true
	default:
		return false
	}
}

func isDisplayField(fieldType FieldType) bool {
	return fieldType == FieldHeading || fieldType == FieldDescription || fieldType == FieldDivider || fieldType == FieldImage || fieldType == FieldSpacer
}

func normalizedEqual(left, right any) bool {
	leftNumber, leftNumeric := toFloat(left)
	rightNumber, rightNumeric := toFloat(right)
	if leftNumeric && rightNumeric {
		return leftNumber == rightNumber
	}
	leftString, leftOK := left.(string)
	rightString, rightOK := right.(string)
	if leftOK && rightOK {
		return strings.EqualFold(normalize.PersianText(leftString), normalize.PersianText(rightString))
	}
	return reflect.DeepEqual(left, right)
}

func toFloat(value any) (float64, bool) {
	switch number := value.(type) {
	case float64:
		return number, !math.IsNaN(number) && !math.IsInf(number, 0)
	case float32:
		converted := float64(number)
		return converted, !math.IsNaN(converted) && !math.IsInf(converted, 0)
	case int:
		return float64(number), true
	case int64:
		return float64(number), true
	case json.Number:
		parsed, err := number.Float64()
		return parsed, err == nil && !math.IsNaN(parsed) && !math.IsInf(parsed, 0)
	case string:
		parsed, err := strconv.ParseFloat(normalize.Digits(strings.TrimSpace(number)), 64)
		return parsed, err == nil && !math.IsNaN(parsed) && !math.IsInf(parsed, 0)
	default:
		return 0, false
	}
}

func toSlice(value any) ([]any, bool) {
	switch values := value.(type) {
	case []any:
		return values, true
	case []string:
		out := make([]any, len(values))
		for index, value := range values {
			out[index] = value
		}
		return out, true
	default:
		return nil, false
	}
}

func collectionValues(value any) ([]any, bool) {
	if values, ok := toSlice(value); ok {
		return values, true
	}
	rows, ok := value.(map[string]any)
	if !ok {
		return nil, false
	}
	values := make([]any, 0, len(rows))
	for _, selected := range rows {
		if multiple, ok := toSlice(selected); ok {
			values = append(values, multiple...)
		} else {
			values = append(values, selected)
		}
	}
	return values, true
}

func comparableTime(value any) (time.Time, bool) {
	text, ok := value.(string)
	if !ok {
		return time.Time{}, false
	}
	for _, layout := range []string{time.DateOnly, "15:04", time.RFC3339} {
		if parsed, err := time.Parse(layout, text); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

func isEmpty(value any) bool {
	if value == nil {
		return true
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed) == ""
	case []any:
		return len(typed) == 0
	case []string:
		return len(typed) == 0
	case map[string]any:
		return len(typed) == 0
	default:
		return false
	}
}

func cloneAnswers(input map[string]any) map[string]any {
	output := make(map[string]any, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}
