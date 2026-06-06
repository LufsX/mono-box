import type { BoxConfigAccessor } from "./box-config";
import type { BoxConfigValues, ProxyMode } from "./clash.types";

export const ALL_PROXY_MODES: ProxyMode[] = ["rule", "global", "direct"];

export type BoxConfigFormValues = BoxConfigValues & {
  toggleModeOrder: ProxyMode[];
};

export function createDefaultBoxConfigFormValues(): BoxConfigFormValues {
  return {
    clashApiPort: 9090,
    clashApiSecret: "",
    toggleAction: "service",
    toggleTunTarget: "toggle",
    toggleModeCycle: ["rule", "global", "direct"],
    toggleModeOrder: ["rule", "global", "direct"],
    controlMode: "disable",
    selectOutbound: "",
    targetCellular: "",
    targetWifi: "",
    targetWifiList: "",
  };
}

export function buildToggleModeOrder(selectedModes: ProxyMode[]): ProxyMode[] {
  const selected = selectedModes.filter((mode): mode is ProxyMode => ALL_PROXY_MODES.includes(mode));
  const remaining = ALL_PROXY_MODES.filter((mode) => !selected.includes(mode));
  return [...selected, ...remaining];
}

export function toBoxConfigFormValues(values: BoxConfigValues): BoxConfigFormValues {
  return {
    ...values,
    toggleModeOrder: buildToggleModeOrder(values.toggleModeCycle),
  };
}

export function quoteBoxConfigValue(value: string): string {
  return `"${value.replace(/\r?\n/g, ";").replace(/\\/g, "\\\\").replace(/"/g, '\\"').replace(/\$/g, "\\$").replace(/`/g, "\\`").trim()}"`;
}

export function serializeBoxConfigFormValues(values: BoxConfigFormValues): Record<string, string> {
  return {
    clash_api_port: values.clashApiPort.toString(),
    clash_api_secret: quoteBoxConfigValue(values.clashApiSecret),
    toggle_action: `"${values.toggleAction}"`,
    toggle_tun_target: `"${values.toggleTunTarget}"`,
    toggle_mode_cycle: `"${values.toggleModeOrder.filter((mode) => values.toggleModeCycle.includes(mode)).join(",")}"`,
    ctr_mode: values.controlMode,
    select_outbound: quoteBoxConfigValue(values.selectOutbound),
    target_cellular: quoteBoxConfigValue(values.targetCellular),
    target_wifi: quoteBoxConfigValue(values.targetWifi),
    target_wifi_list: quoteBoxConfigValue(values.targetWifiList),
  };
}

export function createBoxConfigFormAccessor(boxConfigAccessor: BoxConfigAccessor) {
  async function getBoxConfigForm(): Promise<BoxConfigFormValues> {
    const values = await boxConfigAccessor.getBoxConfig();
    return toBoxConfigFormValues(values);
  }

  async function saveBoxConfigForm(values: BoxConfigFormValues): Promise<void> {
    await boxConfigAccessor.updateBoxConfigValues(serializeBoxConfigFormValues(values));
  }

  return {
    getBoxConfigForm,
    saveBoxConfigForm,
  };
}
