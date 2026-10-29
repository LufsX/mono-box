export interface ConfigPort {
  readBoxConfig(): Promise<string>;
  writeBoxConfig(content: string): Promise<void>;
  updateBoxConfigValues?(updates: Record<string, string>): Promise<void>;
}
