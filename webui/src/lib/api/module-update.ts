export type UpdateSource = "current" | "release" | "nightly";
export const MODULE_ABIS = ["arm64-v8a", "armeabi-v7a", "x86_64", "x86"] as const;
export type ModuleAbi = typeof MODULE_ABIS[number];

export interface UpdateInfo {
  version: string;
  versionCode: number;
  zipUrl: string;
  changelog?: string;
}

const RELEASES_URL = "https://cors.isteed.cc/https://github.com/LufsX/mono-box/releases";

function filename(value: string): string {
  try {
    return decodeURIComponent(new URL(value).pathname.split("/").at(-1) || "");
  } catch {
    return "";
  }
}

export function getModuleAbi(updateJson: string, moduleAbi = ""): ModuleAbi {
  const installed = MODULE_ABIS.find((abi) => abi === moduleAbi);
  if (installed) return installed;
  const name = filename(updateJson);
  return MODULE_ABIS.find((abi) => name === `update-${abi}.json`) || "arm64-v8a";
}

export function getUpdateUrls(source: UpdateSource, currentUpdateJson: string, moduleAbi = ""): string[] {
  const current = currentUpdateJson.trim();
  if (source === "current" && current) return [current];

  const abi = getModuleAbi(current, moduleAbi);
  // Keep the installed repository and proxy when switching GitHub release channels.
  const releases = current.match(/^(https?:\/\/(?:[^/?#]+\/https?:\/\/)?github\.com\/[^/?#]+\/[^/?#]+\/releases)\//i)?.[1] || RELEASES_URL;
  const base = `${releases}/${source === "nightly" ? "download/Prerelease" : "latest/download"}`;
  const architectureUrl = `${base}/update-${abi}.json`;
  // The legacy entry is arm64. Older releases may not have ABI manifests.
  return abi === "arm64-v8a" ? [`${base}/update.json`, architectureUrl] : [architectureUrl];
}

export function normalizeUpdateInfo(raw: unknown, abi: ModuleAbi): UpdateInfo {
  if (!raw || typeof raw !== "object" || Array.isArray(raw)) {
    throw new Error("更新清单格式无效");
  }
  const data = raw as Partial<UpdateInfo>;
  const versionCode = Number(data.versionCode);
  const version = typeof data.version === "string" ? data.version.trim() : "";
  const zipUrl = typeof data.zipUrl === "string" ? data.zipUrl.trim() : "";
  const changelog = typeof data.changelog === "string" ? data.changelog : "";

  let download: URL;
  try {
    download = new URL(zipUrl);
  } catch {
    throw new Error("更新清单中的下载地址无效");
  }
  const numericCode = typeof data.versionCode === "number" || typeof data.versionCode === "string" && /^\d+$/.test(data.versionCode);
  if (!version || !numericCode || !Number.isSafeInteger(versionCode) || versionCode <= 0 || !["http:", "https:"].includes(download.protocol)) {
    throw new Error("更新清单格式无效");
  }

  const name = filename(zipUrl);
  const packageAbi = MODULE_ABIS.find((candidate) => name.endsWith(`_${candidate}.zip`));
  if (packageAbi && packageAbi !== abi) {
    throw new Error(`更新包架构为 ${packageAbi}，当前模块为 ${abi}`);
  }
  return { version, versionCode, zipUrl, changelog };
}

async function readUpdateResponse<T>(
  urls: string[],
  read: (response: Response) => Promise<T>,
  signal?: AbortSignal,
): Promise<T> {
  if (signal?.aborted) throw signal.reason ?? new DOMException("The operation was aborted.", "AbortError");
  const controller = new AbortController();
  let abortReason: unknown;
  const abort = (reason: unknown) => {
    if (controller.signal.aborted) return;
    abortReason = reason;
    controller.abort(reason);
  };
  const cancel = () => abort(signal?.reason ?? new DOMException("The operation was aborted.", "AbortError"));
  signal?.addEventListener("abort", cancel, { once: true });
  const timer = setTimeout(() => abort(new Error("更新请求超时（15 秒），请重试")), 15000);
  try {
    for (const [index, url] of urls.entries()) {
      if (controller.signal.aborted) throw abortReason;
      const response = await fetch(url, { cache: "no-store", signal: controller.signal });
      if (!response.ok) {
        void response.body?.cancel().catch(() => {});
        if (response.status === 404 && index < urls.length - 1) continue;
        throw new Error(`请求失败: HTTP ${response.status}`);
      }
      return await read(response);
    }
    throw new Error("未配置更新通道");
  } catch (error) {
    if (controller.signal.aborted && (error === abortReason || error instanceof Error && error.name === "AbortError")) {
      throw abortReason;
    }
    throw error;
  } finally {
    clearTimeout(timer);
    signal?.removeEventListener("abort", cancel);
  }
}

export async function fetchUpdateInfo(urls: string[], abi: ModuleAbi, signal?: AbortSignal): Promise<UpdateInfo> {
  return readUpdateResponse(urls, async (response) => normalizeUpdateInfo(await response.json(), abi), signal);
}

export async function fetchUpdateChangelog(url: string, signal?: AbortSignal): Promise<string> {
  return readUpdateResponse([url], async (response) => (await response.text()).trim(), signal);
}
