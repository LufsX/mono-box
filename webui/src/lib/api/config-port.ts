export interface ConfigPort {
  readBoxConfig(): Promise<string>;
  writeBoxConfig(content: string): Promise<void>;
}
