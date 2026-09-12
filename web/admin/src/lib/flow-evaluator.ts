import { normalizeDigits } from './persian-date';
import type { ConditionGroup, FlowCondition, FormSchema } from './types';

export interface FlowEvaluation {
  pageIDs: string[];
  endingID: string;
  answers: Record<string, unknown>;
}

export function evaluateFlow(schema: FormSchema, original: Record<string, unknown>): FlowEvaluation {
  const answers = structuredClone(original);
  const pages = new Set(schema.pages.map((page) => page.id));
  const endings = new Set(schema.endings.map((ending) => ending.id));
  const pageIDs: string[] = [];
  let current = schema.flow.start_page_id;
  for (let step = 0; step <= schema.pages.length; step += 1) {
    pageIDs.push(current);
    const outgoing = schema.flow.edges
      .filter((edge) => edge.source_id === current)
      .sort((left, right) => left.priority - right.priority);
    const selected =
      outgoing.find((edge) => !edge.fallback && edge.condition && evaluateGroup(edge.condition, answers)) ??
      outgoing.find((edge) => edge.fallback);
    if (!selected) throw new Error(`صفحهٔ ${current} مسیر منطبق یا پیش‌فرض ندارد.`);
    for (const action of selected.actions ?? []) {
      if (action.mode === 'if_empty' && !isEmpty(answers[action.target_field_id])) continue;
      if (action.source === 'copy') {
        if (action.source_field_id && action.source_field_id in answers)
          answers[action.target_field_id] = structuredClone(answers[action.source_field_id]);
      } else {
        answers[action.target_field_id] = structuredClone(action.value);
      }
    }
    if (endings.has(selected.target_id)) return { pageIDs, endingID: selected.target_id, answers };
    if (!pages.has(selected.target_id)) throw new Error(`مقصد ${selected.target_id} پیدا نشد.`);
    current = selected.target_id;
  }
  throw new Error('ارزیابی مسیر از شمار صفحه‌های فرم عبور کرد؛ احتمال چرخه وجود دارد.');
}

function evaluateGroup(group: ConditionGroup, answers: Record<string, unknown>): boolean {
  const values = [
    ...(group.conditions ?? []).map((condition) => evaluateCondition(condition, answers)),
    ...(group.groups ?? []).map((child) => evaluateGroup(child, answers))
  ];
  return group.match === 'any' ? values.some(Boolean) : values.length > 0 && values.every(Boolean);
}

function evaluateCondition(condition: FlowCondition, answers: Record<string, unknown>): boolean {
  if (condition.operator === 'empty') return !(condition.field_id in answers) || isEmpty(answers[condition.field_id]);
  if (condition.operator === 'not_empty') return condition.field_id in answers && !isEmpty(answers[condition.field_id]);
  if (!(condition.field_id in answers)) return false;
  const actual = answers[condition.field_id];
  const expected = condition.value;
  switch (condition.operator) {
    case 'equals':
      return equal(actual, expected);
    case 'not_equals':
      return !equal(actual, expected);
    case 'contains':
    case 'not_contains': {
      const contains = normalized(actual).includes(normalized(expected));
      return condition.operator === 'contains' ? contains : !contains;
    }
    case 'greater_than':
      return (
        finiteNumber(actual) !== null &&
        finiteNumber(expected) !== null &&
        finiteNumber(actual)! > finiteNumber(expected)!
      );
    case 'less_than':
      return (
        finiteNumber(actual) !== null &&
        finiteNumber(expected) !== null &&
        finiteNumber(actual)! < finiteNumber(expected)!
      );
    case 'before':
      return (
        comparable(actual) !== null && comparable(expected) !== null && comparable(actual)! < comparable(expected)!
      );
    case 'after':
      return (
        comparable(actual) !== null && comparable(expected) !== null && comparable(actual)! > comparable(expected)!
      );
    case 'between': {
      if (!Array.isArray(expected) || expected.length !== 2) return false;
      const numeric = finiteNumber(actual);
      if (numeric !== null) {
        const minimum = finiteNumber(expected[0]);
        const maximum = finiteNumber(expected[1]);
        return minimum !== null && maximum !== null && numeric >= minimum && numeric <= maximum;
      }
      const value = comparable(actual);
      const minimum = comparable(expected[0]);
      const maximum = comparable(expected[1]);
      return value !== null && minimum !== null && maximum !== null && value >= minimum && value <= maximum;
    }
    case 'contains_any':
    case 'contains_all': {
      const selected = collectionValues(actual);
      if (!selected || !Array.isArray(expected)) return false;
      const matches = expected.map((item) => selected.some((candidate) => equal(candidate, item)));
      return condition.operator === 'contains_all' ? matches.every(Boolean) : matches.some(Boolean);
    }
    default:
      return false;
  }
}

function equal(left: unknown, right: unknown): boolean {
  const leftNumber = finiteNumber(left);
  const rightNumber = finiteNumber(right);
  if (leftNumber !== null && rightNumber !== null) return leftNumber === rightNumber;
  if (typeof left === 'string' && typeof right === 'string') return normalized(left) === normalized(right);
  return JSON.stringify(left) === JSON.stringify(right);
}

function finiteNumber(value: unknown): number | null {
  if (typeof value !== 'number' && typeof value !== 'string') return null;
  if (typeof value === 'string' && value.trim() === '') return null;
  const parsed = Number(normalizeDigits(String(value).trim()));
  return Number.isFinite(parsed) ? parsed : null;
}

function comparable(value: unknown): string | null {
  if (typeof value !== 'string' || !/^\d{2,4}[-/:]\d{1,2}([-/.:T]\d{1,2})?/.test(value)) return null;
  return value;
}

function normalized(value: unknown): string {
  return normalizeDigits(String(value ?? ''))
    .replace(/ي/g, 'ی')
    .replace(/ك/g, 'ک')
    .trim()
    .toLocaleLowerCase('fa');
}

function isEmpty(value: unknown): boolean {
  return (
    value == null ||
    value === '' ||
    (Array.isArray(value) && value.length === 0) ||
    (isRecord(value) && Object.keys(value).length === 0)
  );
}

function collectionValues(value: unknown): unknown[] | null {
  if (Array.isArray(value)) return value;
  if (!isRecord(value)) return null;
  return Object.values(value).flatMap((selected) => (Array.isArray(selected) ? selected : [selected]));
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}
