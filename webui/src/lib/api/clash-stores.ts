import { writable } from "svelte/store";
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

  async function refreshCoreStatus() {
    coreStatus.set(await clashApi.checkStatus());
  }

  async function refreshConfigs() {
    try {
      const configData = await clashApi.getConfigs();
      configs.set(configData);
      if (configData?.mode) currentMode.set(configData.mode as ProxyMode);
    } catch {
      configs.set(null);
    }
  }

  async function refreshVersion() {
    try {
      versionCheck.set(await clashApi.checkVersion());
    } catch {
      versionCheck.set(null);
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
