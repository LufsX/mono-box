import type { ConfigPort } from "./config-port";
import { parseBoxConfig, upsertConfigValue } from "./config-parser";
import { getErrorMessage } from "./error-utils";
import type { BoxConfigValues } from "./clash.types";

export type { BoxConfigValues } from "./clash.types";

export function createBoxConfigAccessor(configPort: ConfigPort) {
  let cachedContent: string | null = null;

  function clearBoxConfigCache(): void {
    cachedContent = null;
  }

  async function getBoxConfig(): Promise<BoxConfigValues> {
    const content = await getBoxConfigRaw();
    return parseBoxConfig(content);
  }

  async function getBoxConfigRaw(): Promise<string> {
    if (cachedContent !== null) {
      return cachedContent;
    }
    cachedContent = await configPort.readBoxConfig();
    return cachedContent;
  }

  async function updateBoxConfigValues(updates: Record<string, string>): Promise<void> {
    try {
      let content = await getBoxConfigRaw();

      for (const [key, value] of Object.entries(updates)) {
        content = upsertConfigValue(content, key, value);
      }

      await configPort.writeBoxConfig(content);
      cachedContent = content;
    } catch (e) {
      console.error("Failed to update box.config values:", e);
      throw new Error(getErrorMessage(e));
    }
  }

  return {
    clearBoxConfigCache,
    getBoxConfig,
    getBoxConfigRaw,
    updateBoxConfigValues,
  };
}

export type BoxConfigAccessor = ReturnType<typeof createBoxConfigAccessor>;
