import type { ClashApiPort } from "./clash.types";
import { createClashApi } from "./clash";
import { createMockClashApi } from "./clash.mock";
import * as actionReal from "./action";
import * as actionMock from "./action.mock";
import { createBoxConfigAccessor } from "./box-config";
import { createBoxConfigFormAccessor } from "./box-config-form";
import { createClashStores } from "./clash-stores";

const isProduction = import.meta.env.MODE === "production";

const activeAction = isProduction ? actionReal : actionMock;
const configPort = activeAction.configPortAdapter;

export const clashApi: ClashApiPort = (isProduction ? createClashApi : createMockClashApi)(configPort);
export const actionApi = activeAction;

const boxConfigAccessor = createBoxConfigAccessor(configPort);
export const { getBoxConfig, getBoxConfigRaw, updateBoxConfigValues, clearBoxConfigCache } = boxConfigAccessor;

const boxConfigFormAccessor = createBoxConfigFormAccessor(boxConfigAccessor);
export const { getBoxConfigForm, saveBoxConfigForm } = boxConfigFormAccessor;

const clashStores = createClashStores(clashApi);
export const { stores, actions } = clashStores;

export { parseBoxConfig, upsertConfigValue, readConfigValue } from "./config-parser";
export { normalizeMihomoConfigName } from "./action";
export { ALL_PROXY_MODES, buildToggleModeOrder, createDefaultBoxConfigFormValues, type BoxConfigFormValues } from "./box-config-form";
export type { MihomoConfigFile, MihomoConfigFileKind } from "./action";

export type {
  ClashApiPort,
  ClashConfig,
  ClashLogEntry,
  ClashLogLevel,
  MemoryData,
  TrafficData,
  ClashProxyHistory,
  ClashProxy,
  ClashProxyMap,
  ClashProxyProvider,
  ClashProxyProviderMap,
  ClashRuleProvider,
  ClashRuleProviderMap,
  ControlMode,
  ProxyMode,
  BoxConfigValues,
  ClashVersionCheckResult,
  ClashConnectionMetadata,
  ClashConnection,
  ClashConnectionsResponse,
  ClashRule,
  ClashRulesResponse,
  ClashConfigs,
  UpgradeResult,
  ClashProxyDetail,
  CoreStatusResult,
  CommandResult,
  ModuleInfo,
} from "./clash.types";
