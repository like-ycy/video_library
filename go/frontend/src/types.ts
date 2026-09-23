export interface Library {
  id: string;
  root: string;
  available: boolean;
}
export interface Video {
  id: number;
  libraryId: string;
  actress: string;
  fanha: string;
  stem: string;
  title: string;
  releaseDate: string;
  siteLengthMin: number | null;
  durationMs: number;
  genres: string[] | null;
  cast: string[] | null;
  fileSize: number;
  width: number;
  height: number;
  vcodec: string;
  acodec: string;
  missing: boolean;
  scraped: boolean;
  scrapedAt: string;
  favorite: boolean;
  rating: number | null;
  watchPositionMs: number;
  playCount: number;
  lastPlayedAt?: string;
  coverUrl: string;
  shotUrls: string[] | null;
  videoUrl: string;
  playable: boolean;
}
export interface Config {
  concurrency: number;
  scrapeTimeoutMin: number;
  scraperPath: string;
  ffprobePath: string;
  playerPath: string;
  theme: string;
}
export interface Health {
  v: number;
  scraper: string;
  python: string;
  chrome: { found: boolean; path: string };
  driver: {
    ready: boolean;
    writable: boolean;
    dir: string;
    on_demand: boolean;
  };
}
export interface Candidate {
  actress: string;
  fanha: string;
  stem: string;
  videoFile: string;
  fileSize: number;
  scraped: boolean;
  missingArt: boolean;
}
export interface Issue {
  kind: string;
  actress: string;
  subject: string;
  message: string;
}
export interface ScanResult {
  candidates: Candidate[] | null;
  issues: Issue[] | null;
}
export interface Paths {
  configDir: string;
  configFile: string;
  appData: string;
}
export interface EnvReport {
  platform: string;
  goVersion: string;
  ffprobeOk: boolean;
  ffprobePath: string;
  scraperExeOk: boolean;
  scraperExePath: string;
  playerPath: string;
  scraperHealth?: Health;
  scraperError?: string;
  tempWritable: boolean;
}
export interface Filter {
  Actress: string;
  Keyword: string;
  Genres: string[];
  FavoriteOnly: boolean;
  IncludeMissing: boolean;
  MinDurationMs: number;
}
export interface ImportStats {
  Found: number;
  Probed: number;
  MediaCached: number;
  Vanished: number;
  Issues: unknown[] | null;
}
export interface Backend {
  ListLibraries(): Promise<Library[]>;
  PickLibraryRoot(): Promise<string>;
  AddLibrary(path: string): Promise<Library>;
  RemoveLibrary(id: string): Promise<void>;
  QueryVideos(
    id: string,
    filter: Filter,
    sort: { Field: string; Desc: boolean },
    page: number,
    size: number,
  ): Promise<{ items: Video[]; total: number }>;
  ListActresses(
    id: string,
  ): Promise<{ actress: string; total: number; missing: number }[]>;
  ListGenres(id: string): Promise<string[]>;
  ListContinueWatching(id: string): Promise<Video[]>;
  ListRecentPlays(id: string): Promise<Video[]>;
  ClearRecentHistory(id: string): Promise<void>;
  ClearProgress(id: string, video: number): Promise<void>;
  ToggleFavorite(id: string, video: number): Promise<boolean>;
  SetRating(id: string, video: number, rating: number | null): Promise<void>;
  SaveProgress(id: string, video: number, position: number): Promise<void>;
  PlayEmbedded(id: string, video: number): Promise<void>;
  OpenInPlayer(id: string, video: number, resume: boolean): Promise<void>;
  ScanLibrary(id: string): Promise<ScanResult>;
  ImportLibrary(id: string): Promise<ImportStats>;
  RebuildIndex(id: string): Promise<ImportStats>;
  StartScrape(id: string, stems: string[], force: boolean): Promise<void>;
  CancelScrape(): Promise<boolean>;
  ScrapeStatus(): Promise<boolean>;
  GetScrapeLog(): Promise<string[]>;
  ScraperHealth(): Promise<Health>;
  GetConfig(): Promise<Config>;
  SaveConfig(config: Config): Promise<Config>;
  Paths(): Promise<Paths>;
  DiagnoseEnv(): Promise<EnvReport>;
  PickExecutable(title: string): Promise<string>;
  GetSystemAppearance(): Promise<string>;
}
export interface ScrapeEvent {
  fanha: string;
  stage?: string;
  percent?: number;
  title?: string;
  shots?: number;
  totalShots?: number;
  reason?: string;
  message?: string;
  detail?: string;
  retryable?: boolean;
}
export interface FinishedEvent {
  ok: number;
  failed: number;
  skipped: number;
  canceled: boolean;
  fatal?: string;
  fatalMessage?: string;
  error?: string;
}
export interface Events {
  "scrape:progress": ScrapeEvent;
  "scrape:item-done": ScrapeEvent;
  "scrape:item-failed": ScrapeEvent;
  "scrape:finished": FinishedEvent;
  "index:progress": { done: number; total: number };
  "index:done": { actresses: number };
  "index:failed": { message: string };
}
export interface Runtime {
  Environment(): Promise<{ platform: string }>;
  WindowMinimise(): void;
  WindowIsMaximised(): Promise<boolean>;
  WindowUnmaximise(): void;
  WindowMaximise(): void;
  Quit(): void;
  EventsOn<T>(event: string, callback: (payload: T) => void): () => void;
}
declare global {
  interface Window {
    go?: { main: { App: Backend } };
    runtime?: Runtime;
  }
}
