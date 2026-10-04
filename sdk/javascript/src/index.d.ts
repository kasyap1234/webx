// Type definitions for webx-sdk

export interface WebXOptions {
  baseUrl?: string;
  apiKey?: string;
  timeout?: number;
}

export interface ScrapeOptions {
  formats?: string[];
  render?: boolean;
  autoRender?: boolean;
  browser?: boolean;
  actions?: string;
  waitFor?: string;
  scrolls?: number;
  screenshot?: boolean;
  proxy?: string;
  session?: string;
  pageSession?: string;
  stealth?: boolean;
  profile?: string;
  blockAds?: boolean;
  textMode?: boolean;
  mobile?: boolean;
  locale?: string;
  timezone?: string;
  captureNetwork?: boolean;
  captureConsole?: boolean;
  skipTlsVerification?: boolean;
  pierceDom?: boolean;
  scrollSelector?: string;
  scrollBy?: number;
  engine?: "chrome" | "light" | "lightpanda";
  cookies?: string;
  retryAfter?: boolean;
  fit?: string;
  maxChars?: number;
  maxTokens?: number;
  includeTags?: string[];
  excludeTags?: string[];
  maxAge?: number;
  changeTracking?: boolean;
  zdr?: boolean;
  /** Screenshot tuning — requires formats:["screenshot"] + render. */
  screenshotOptions?: {
    fullPage?: boolean;
    quality?: number;            // JPEG 0-100
    viewport?: { width?: number; height?: number };
  };
  /** Preferred transcript caption language (formats:["transcript"]). */
  lang?: string;
}

export interface SearchOptions {
  limit?: number;
  providers?: string[];
  site?: string;
  domains?: string[];        // Exa includeDomains
  excludeDomains?: string[];
  scrape?: boolean;
  scrapeChars?: number;
  highlightsOnly?: boolean;  // excerpts only, no full content
  rerank?: boolean;
  semantic?: boolean;
  lang?: string;
  topic?: string;
  after?: string;            // RFC3339 or YYYY-MM-DD — Exa startPublishedDate
  before?: string;
  exact?: boolean;
  render?: boolean;
  autoRender?: boolean;
  browser?: boolean;
  session?: string;
  /** Result tier: "fast" | "basic" | "advanced" (Tavily search_depth). */
  depth?: "fast" | "basic" | "advanced";
  /** Result verticals — default ["web"]. */
  sources?: Array<"web" | "news" | "images">;
  /** Subpages to crawl per top result (Exa subpages). */
  subpages?: number;
  /** Keywords steering which subpages are picked. */
  subpageTarget?: string[];
  /** Named local-index corpus to include. */
  collection?: string;
}

export interface AnswerOptions {
  num?: number;              // sources to cite (default 5)
  providers?: string[];
  domains?: string[];
  llm?: boolean;             // WEBX_LLM_* synthesis — answer_type:"llm" on success
  maxTokens?: number;
}

export interface Citation {
  n: number;
  url: string;
  title: string;
  highlights?: string[];
  snippet?: string;
  published?: string;
}

export interface AnswerResponse {
  success: boolean;
  answer: string | null;
  answer_type: "llm" | "extractive";
  evidence_md: string;
  citations: Citation[];
  llm_error?: string;
  errors?: Record<string, string>;
  provider_ms?: Record<string, number>;
  cache_hit?: boolean;
}

export interface ExtractOptions {
  schema?: Record<string, unknown>;
  prompt?: string;
  css?: Record<string, string>;
  type?: "product" | "article" | "job" | "event" | "faq";
  render?: boolean;
  /** Batch URLs — or pass url:"example.com/*" for a wildcard crawl+extract. */
  urls?: string[];
  /** Queue as a job — result lands in Job.result. */
  async?: boolean;
  wait?: boolean;
  webhookUrl?: string;
  webhookSecret?: string;
}

export interface AgentOptions {
  /** Optional site constraint — agent stays under this host. */
  url?: string;
  /** JSON-schema for structured output. */
  schema?: Record<string, unknown>;
  /** Candidate pages to read (default 5). */
  limit?: number;
  render?: boolean;
  webhookUrl?: string;
  webhookSecret?: string;
  /** false → returns a Job handle instead of the result. */
  wait?: boolean;
}

export interface CrawlOptions {
  goal?: string;
  limit?: number;
  depth?: number;
  semantic?: boolean;
  concurrency?: number;
  webhookUrl?: string;
  /** HMAC-SHA256 key — webhook payloads arrive signed (X-Webx-Signature). */
  webhookSecret?: string;
  includePaths?: string[];
  excludePaths?: string[];
  wait?: boolean;
}

export interface KeyOptions {
  role?: "member" | "admin";
  rpm?: number;
  concurrency?: number;
  monthlyUnits?: number;
}

export interface ScheduleOptions {
  name: string;
  cron: string;
  action: "crawl" | "batch";
  params?: Record<string, unknown>;
  timezone?: string;
  webhookUrl?: string;
}

export interface Document {
  url: string;
  final_url?: string;
  title?: string;
  markdown?: string;
  html?: string;
  links?: string[];
  images?: string[];
  metadata?: Record<string, unknown>;
  tier_used?: string;
  duration_ms?: number;
  warnings?: string[];
  [key: string]: unknown;
}

export interface ApiResponse<T = unknown> {
  success: boolean;
  data?: T;
  errors?: Record<string, string>;
}

export declare class WebXError extends Error {
  status: number;
  code: string;
  payment: Record<string, unknown> | null;
}

export declare class Job {
  constructor(client: WebX, id: string);
  id: string;
  status: string;
  data: Array<Record<string, unknown>>;
  /** research/extract/agent payloads — populated on completion. */
  result?: unknown;
  poll(opts?: {
    interval?: number;
    timeout?: number;
    onTick?: (status: string, done: number, total: number) => void;
  }): Promise<Job>;
  errors(): Promise<ApiResponse>;
  cancel(): Promise<ApiResponse>;
}

export declare class WebX {
  constructor(opts?: WebXOptions);
  scrape(url: string, opts?: ScrapeOptions): Promise<ApiResponse<Document>>;
  search(query: string, opts?: SearchOptions): Promise<ApiResponse>;
  answer(query: string, opts?: AnswerOptions): Promise<AnswerResponse>;
  map(url: string, opts?: { limit?: number }): Promise<ApiResponse>;
  extract(url: string, opts?: ExtractOptions): Promise<ApiResponse | Job>;
  research(question: string, opts?: {
    sources?: number;
    outputSchema?: Record<string, unknown>;
    async?: boolean;
    wait?: boolean;
    webhookUrl?: string;
    webhookSecret?: string;
  }): Promise<ApiResponse | Job>;
  agent(goal: string, opts?: AgentOptions): Promise<ApiResponse | Job>;
  verify(claim: string): Promise<ApiResponse>;
  similar(url: string, opts?: { limit?: number; web?: boolean }): Promise<ApiResponse>;
  wayback(url: string, opts?: { at?: string }): Promise<ApiResponse>;
  crawl(url: string, opts?: CrawlOptions): Promise<Job>;
  batchScrape(urls: string[], options?: Record<string, unknown>,
    opts?: { wait?: boolean }): Promise<Job>;
  createKey(name: string, opts?: KeyOptions): Promise<ApiResponse>;
  keys(): Promise<ApiResponse>;
  updateKey(id: string, fields: Partial<KeyOptions> & {
    name?: string; disabled?: boolean;
  }): Promise<ApiResponse>;
  deleteKey(id: string): Promise<ApiResponse>;
  usage(opts?: { days?: number; key?: string }): Promise<ApiResponse>;
  audit(limit?: number): Promise<ApiResponse>;
  schedule(opts: ScheduleOptions): Promise<ApiResponse>;
  schedules(): Promise<ApiResponse>;
  updateSchedule(id: string, fields: Partial<ScheduleOptions> & {
    enabled?: boolean;
  }): Promise<ApiResponse>;
  deleteSchedule(id: string): Promise<ApiResponse>;
  jobs(): Promise<ApiResponse>;
  jobErrors(jobId: string): Promise<ApiResponse>;
  cancel(jobId: string): Promise<ApiResponse>;
  doctor(): Promise<ApiResponse>;
  health(): Promise<ApiResponse>;
  ready(): Promise<ApiResponse>;
  version(): Promise<ApiResponse>;
}
