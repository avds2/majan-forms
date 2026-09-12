export type Role = 'admin' | 'designer' | 'viewer';

export interface RuntimeConfig {
  timezone: string;
}

export interface User {
  id: string;
  email: string;
  username: string;
  display_name: string;
  role: Role;
  active: boolean;
  last_login_at?: string;
  created_at: string;
  updated_at: string;
}

export interface SystemHealth {
  app: { version: string; commit: string; build_date: string };
  schema: { current: number; latest: number; pending: number };
  database: string;
  maintenance: {
    write_locked: boolean;
    reason: string;
    locked_at?: string;
    lease_until?: string;
    active_writes: number;
  };
  data_disk: { path: string; total_bytes: number; free_bytes: number; writable: boolean };
  backup_disk: { path: string; total_bytes: number; free_bytes: number; writable: boolean };
  latest_backup?: { name: string; created_at: string; created_at_jalali: string; size: number; sha256: string };
}

export interface SystemBackup {
  name: string;
  valid: boolean;
  error?: string;
  size: number;
  sha256: string;
  created_at: string;
  created_at_jalali?: string;
  app_version: string;
  schema_version: number;
  files: number;
}

export interface SystemBackupJob {
  running: boolean;
  started_at?: string;
  started_at_jalali?: string;
  completed_at?: string;
  completed_at_jalali?: string;
  error?: string;
  backup?: SystemBackup;
}

export interface SystemBackupList {
  items: SystemBackup[];
  job: SystemBackupJob;
}

export interface SystemRestoreRequest {
  pending: boolean;
  backup: SystemBackup;
  message: string;
}

export type ProjectStatus = 'draft' | 'published' | 'closed' | 'trash';

export interface Project {
  id: string;
  title: string;
  slug: string;
  status: ProjectStatus;
  active_version_id?: string;
  created_by: string;
  updated_by: string;
  created_at: string;
  updated_at: string;
  deleted_at?: string;
}

export type FieldType =
  | 'short_text'
  | 'long_text'
  | 'email'
  | 'phone'
  | 'url'
  | 'full_name'
  | 'address'
  | 'national_id'
  | 'number'
  | 'money'
  | 'percent'
  | 'rating'
  | 'scale'
  | 'nps'
  | 'hidden'
  | 'calculated'
  | 'yes_no'
  | 'single_choice'
  | 'multi_choice'
  | 'dropdown'
  | 'multi_dropdown'
  | 'matrix'
  | 'persian_date'
  | 'time'
  | 'datetime'
  | 'date_range'
  | 'file'
  | 'signature'
  | 'consent'
  | 'heading'
  | 'description'
  | 'divider'
  | 'image'
  | 'spacer';

export interface FormOption {
  id: string;
  label: string;
}

export interface FormField {
  id: string;
  type: FieldType;
  label: string;
  help_text?: string;
  required?: boolean;
  placeholder?: string;
  width?: 'full' | 'half';
  content?: string;
  default_value?: unknown;
  options?: FormOption[];
  text?: { minimum_length?: number; maximum_length?: number; pattern?: string; national_id?: boolean };
  number?: { minimum?: number; maximum?: number; integer?: boolean };
  date?: { minimum?: string; maximum?: string };
  scale?: { minimum: number; maximum: number; low_label?: string; high_label?: string };
  matrix?: { rows: FormOption[]; columns: FormOption[]; multiple?: boolean };
  file?: { maximum_bytes: number; maximum_files: number; extensions: string[] };
  consent?: { agreement_text: string; link_label?: string; link_url?: string };
  image?: { source: string; alt: string };
}

export interface FormPage {
  id: string;
  title: string;
  description?: string;
  fields: FormField[];
}

export interface FormEnding {
  id: string;
  title: string;
  message?: string;
}

export interface FlowCondition {
  field_id: string;
  operator: string;
  value?: unknown;
}

export interface ConditionGroup {
  match: 'all' | 'any';
  conditions?: FlowCondition[];
  groups?: ConditionGroup[];
}

export interface AssignmentAction {
  id: string;
  target_field_id: string;
  mode: 'replace' | 'if_empty';
  source: 'constant' | 'copy';
  source_field_id?: string;
  value?: unknown;
}

export interface FlowEdge {
  id: string;
  source_id: string;
  target_id: string;
  priority: number;
  fallback?: boolean;
  condition?: ConditionGroup;
  actions?: AssignmentAction[];
}

export interface FormSchema {
  schema_version: 1;
  title: string;
  pages: FormPage[];
  endings: FormEnding[];
  flow: { start_page_id: string; edges: FlowEdge[] };
  theme: { preset: 'minimal' | 'formal' | 'warm'; primary_color?: string; alignment?: 'start' | 'center' };
  public_settings: { minimum_fill_seconds: number; turnstile_enabled?: boolean };
  editor_metadata: Record<string, unknown>;
}

export interface Draft {
  project_id: string;
  schema: FormSchema;
  revision: number;
  checksum: string;
  updated_by: string;
  updated_at: string;
}

export interface FormVersion {
  id: string;
  project_id: string;
  version_number: number;
  schema: FormSchema;
  checksum: string;
  published_by: string;
  published_at: string;
}

export interface PublishValidation {
  valid: true;
  revision: number;
  checksum: string;
}

export interface ResponseSummary {
  id: string;
  public_id: string;
  version_id: string;
  version_number: number;
  ending_id: string;
  ending_title: string;
  started_at: string;
  submitted_at: string;
  submitted_at_jalali: string;
  duration_seconds: number;
  deleted_at?: string;
  deleted_at_jalali?: string;
}

export interface ResponseFile {
  id: string;
  name: string;
  size_bytes: number;
  mime: string;
  checksum: string;
  download_path: string;
}

export interface ResponseAnswer {
  field_id: string;
  field_type: FieldType;
  label: string;
  page_title: string;
  value?: unknown;
  display_value: string;
  files?: ResponseFile[];
}

export interface ResponseDetail extends ResponseSummary {
  project_id: string;
  project_title: string;
  answers: ResponseAnswer[];
}

export interface ResponseList {
  items: ResponseSummary[];
  pagination: { page: number; page_size: number; total: number; total_pages: number };
}

export interface ResponseQuery {
  page?: number;
  page_size?: number;
  search?: string;
  version?: number;
  ending?: string;
  from?: string;
  to?: string;
  deleted?: 'active' | 'deleted' | 'all';
  sort?: 'newest' | 'oldest' | 'duration_desc' | 'duration_asc';
}

export interface ReportQuery {
  version?: number;
  ending?: string;
  field?: string;
  field_value?: string;
  from?: string;
  to?: string;
}

export interface ReportKPI {
  views: number;
  starts: number;
  completes: number;
  completion_rate: number;
  average_duration_seconds: number;
}

export interface TrendPoint {
  date: string;
  date_jalali: string;
  views: number;
  starts: number;
  completes: number;
}

export interface CountBreakdown {
  version_number: number;
  across_versions?: boolean;
  id: string;
  label: string;
  count: number;
}

export interface ChoiceBreakdown {
  version_number: number;
  across_versions?: boolean;
  field_id: string;
  field_label: string;
  values: Array<{ value: string; label: string; count: number }>;
}

export interface NumericBreakdown {
  version_number: number;
  across_versions?: boolean;
  field_id: string;
  field_label: string;
  field_type: FieldType;
  count: number;
  minimum: number;
  maximum: number;
  average: number;
  median: number;
  nps?: number;
}

export interface ProjectReport {
  project_id: string;
  project_title: string;
  generated_at: string;
  generated_at_jalali: string;
  kpi: ReportKPI;
  trend: TrendPoint[];
  endings: CountBreakdown[];
  page_activity: CountBreakdown[];
  choices: ChoiceBreakdown[];
  numbers: NumericBreakdown[];
  versions: Array<{ id: string; version_number: number; published_at: string }>;
  fields: Array<{ id: string; label: string; type: FieldType; options?: Array<{ id: string; label: string }> }>;
  ending_options: Array<{ id: string; label: string }>;
  segmented: boolean;
}

export interface ProjectPackageVersionPreview {
  version_number: number;
  published_at: string;
  checksum: string;
}

export interface ProjectPackagePreview {
  format: 'project-package-v1';
  format_version: 1;
  app_version: string;
  created_at: string;
  package_sha256: string;
  source_project_id: string;
  source_title: string;
  source_slug: string;
  draft_revision: number;
  page_count: number;
  field_count: number;
  ending_count: number;
  version_count: number;
  asset_count: number;
  versions: ProjectPackageVersionPreview[];
  includes_responses: false;
  imported_project_status: 'draft';
}

export interface ProjectPackageImportResult {
  project: Project;
  imported_versions: number;
  package_sha256: string;
}

export type AIAPIMode = 'responses' | 'chat_completions';

export interface AIProviderSettings {
  base_url: string;
  api_mode: AIAPIMode;
  model: string;
  timeout_seconds: number;
  max_output_tokens: number;
  allow_private_endpoint: boolean;
  has_api_key: boolean;
  compatible: boolean;
  compatibility_tested_at?: string;
  updated_at: string;
}

export interface AIProviderCompatibility {
  api_mode: AIAPIMode;
  model: string;
  streaming: boolean;
  tools: boolean;
  latency_ms: number;
}

export interface AIProjectSettings {
  project_id: string;
  enabled: boolean;
  text_sampling_enabled: boolean;
  provider_configured: boolean;
}

export interface AIConversation {
  id: string;
  project_id: string;
  created_by: string;
  title: string;
  created_at: string;
  updated_at: string;
}

export interface AIMessage {
  id: string;
  conversation_id: string;
  role: 'user' | 'assistant';
  content: string;
  context_summary?: { tools?: string[]; sampled_text?: boolean };
  created_at: string;
}

export interface APIErrorBody {
  error?: {
    code?: string;
    message?: string;
    details?: Array<{ path: string; message: string }>;
    conflict?: {
      current_revision: number;
      updated_by: string;
      updated_at: string;
    };
  };
}
