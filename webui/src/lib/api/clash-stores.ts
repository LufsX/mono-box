import { writable } from "svelte/store";
import { classifyConnectionError } from "./error-utils";
import type { ClashApiPort } from "./clash.types";
import type { CoreStatusResult, ClashProxyMap, ClashProxyProviderMap, ClashRuleProviderMap, ClashRulesResponse, ClashConfigs, ClashVersionCheckResult, ProxyMode } from "./clash.types";

export function createClashStores(clashApi: ClashApiPort) {
  const coreStatus = writable<CoreStatusResult | null>(null);
  const currentMode = writable<ProxyMode>("rule");
  const proxies = writable<ClashProxyMap | null>(null);
  const proxyProviders = writable<ClashProxyProviderMap | null>(null);
  const ruleProviders = writable<ClashRuleProviderMap | null>(null);
  const rules = writable<ClashRulesResponse | null>(null);
  const configs = writable<ClashConfigs | null>(null);
  const versionCheck = writable<ClashVersionCheckResult | null>(null);
  let configSequence = 0;
  let versionSequence = 0;

  async function refreshCoreStatus() {
    return refreshConfigs();
  }

  async function refreshConfigs() {
    const sequence = ++configSequence;
    try {
      const configData = await clashApi.getConfigs();
      const status: CoreStatusResult = { ok: true };
      if (sequence === configSequence) {
        configs.set(configData);
        if (configData?.mode) currentMode.set(configData.mode as ProxyMode);
        coreStatus.set(status);
      }
      return status;
    } catch (error) {
      const status = classifyConnectionError(error);
      if (sequence === configSequence) {
        configs.set(null);
        coreStatus.set(status);
      }
      return status;
    }
  }

  async function refreshVersion() {
    const sequence = ++versionSequence;
    try {
      const version = await clashApi.checkVersion();
      if (sequence === versionSequence) versionCheck.set(version);
    } catch {
      if (sequence === versionSequence) versionCheck.set(null);
    }
  }

  async function refreshProxies() {
    const [proxyData, configData] = await Promise.all([clashApi.getProxies(), clashApi.getConfigs()]);
    proxies.set(proxyData);
    if (configData?.mode) currentMode.set(configData.mode as ProxyMode);
  }

  async function refreshProxyProviders() {
    proxyProviders.set(await clashApi.getProxyProviders());
  }

  async function refreshRules() {
    const [rulesData, provData, proxyData] = await Promise.all([clashApi.getRules(), clashApi.getRuleProviders(), clashApi.getProxies()]);
    rules.set(rulesData);
    ruleProviders.set(provData);
    proxies.set(proxyData);
  }

  async function switchClashMode(mode: ProxyMode) {
    await clashApi.setMode(mode);
    currentMode.set(mode);
  }

  return {
    stores: { coreStatus, currentMode, proxies, proxyProviders, ruleProviders, rules, configs, versionCheck },
    actions: {
      refreshCoreStatus,
      refreshConfigs,
      refreshVersion,
      refreshProxies,
      refreshProxyProviders,
      refreshRules,
      switchClashMode,
    },
  };
}
