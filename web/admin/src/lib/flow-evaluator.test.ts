import { describe, expect, it } from 'vitest';
import goldenCases from '../../../../testdata/flow-cases.json';
import { evaluateFlow } from './flow-evaluator';
import type { FormSchema } from './types';

const pageStart = '11111111-1111-4111-8111-111111111111';
const pageWork = '22222222-2222-4222-8222-222222222222';
const pageShort = '33333333-3333-4333-8333-333333333333';
const experience = '11111111-aaaa-4111-8111-111111111111';
const portfolio = '22222222-aaaa-4222-8222-222222222222';
const track = '33333333-aaaa-4333-8333-333333333333';
const hired = 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa';
const rejected = 'bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb';

const schema: FormSchema = {
  schema_version: 1,
  title: 'فرم استخدام',
  pages: [
    {
      id: pageStart,
      title: 'سابقه',
      fields: [
        { id: experience, type: 'number', label: 'سال سابقه' },
        { id: track, type: 'hidden', label: 'مسیر ارزیابی' }
      ]
    },
    {
      id: pageWork,
      title: 'نمونه‌کار',
      fields: [{ id: portfolio, type: 'single_choice', label: 'نمونه‌کار تأیید شد؟' }]
    },
    { id: pageShort, title: 'مسیر کوتاه', fields: [] }
  ],
  endings: [
    { id: hired, title: 'دعوت به مصاحبه' },
    { id: rejected, title: 'پایان بررسی' }
  ],
  flow: {
    start_page_id: pageStart,
    edges: [
      {
        id: '10000000-0000-4000-8000-000000000001',
        source_id: pageStart,
        target_id: pageWork,
        priority: 10,
        condition: { match: 'all', conditions: [{ field_id: experience, operator: 'greater_than', value: 4 }] },
        actions: [
          {
            id: '10000000-0000-4000-8000-000000000011',
            target_field_id: track,
            mode: 'replace',
            source: 'constant',
            value: 'ارشد'
          }
        ]
      },
      {
        id: '10000000-0000-4000-8000-000000000002',
        source_id: pageStart,
        target_id: pageShort,
        priority: 1000,
        fallback: true
      },
      {
        id: '10000000-0000-4000-8000-000000000003',
        source_id: pageWork,
        target_id: hired,
        priority: 10,
        condition: {
          match: 'all',
          conditions: [{ field_id: portfolio, operator: 'equals', value: '44444444-aaaa-4444-8444-444444444444' }]
        }
      },
      {
        id: '10000000-0000-4000-8000-000000000004',
        source_id: pageWork,
        target_id: rejected,
        priority: 1000,
        fallback: true
      },
      {
        id: '10000000-0000-4000-8000-000000000005',
        source_id: pageShort,
        target_id: rejected,
        priority: 1000,
        fallback: true
      }
    ]
  },
  theme: { preset: 'minimal', alignment: 'start' },
  public_settings: { minimum_fill_seconds: 2 },
  editor_metadata: {}
};

describe('shared deterministic flow cases', () => {
  for (const testCase of goldenCases) {
    it(testCase.name, () => {
      const result = evaluateFlow(schema, testCase.answers);
      expect(result.pageIDs).toEqual(testCase.pages);
      expect(result.endingID).toBe(testCase.ending);
      expect(result.answers[track] ?? null).toEqual(testCase.assigned_track);
    });
  }

  it('flattens matrix selections for collection conditions', () => {
    const matrixSchema = structuredClone(schema);
    matrixSchema.flow.edges[0].condition = {
      match: 'all',
      conditions: [{ field_id: 'matrix', operator: 'contains_all', value: ['column-one', 'column-two'] }]
    };
    const result = evaluateFlow(matrixSchema, {
      matrix: { 'row-one': 'column-one', 'row-two': ['column-two'] }
    });
    expect(result.pageIDs).toEqual([pageStart, pageWork]);
  });

  it('treats a missing answer as empty, not non-empty', () => {
    const emptySchema = structuredClone(schema);
    emptySchema.flow.edges[0].condition = {
      match: 'all',
      conditions: [{ field_id: experience, operator: 'empty' }]
    };
    expect(evaluateFlow(emptySchema, {}).pageIDs).toEqual([pageStart, pageWork]);
    emptySchema.flow.edges[0].condition = {
      match: 'all',
      conditions: [{ field_id: experience, operator: 'not_empty' }]
    };
    expect(evaluateFlow(emptySchema, {}).pageIDs).toEqual([pageStart, pageShort]);
  });
});
