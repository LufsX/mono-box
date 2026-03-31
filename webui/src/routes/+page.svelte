<script lang="ts">
  import { exec, enableEdgeToEdge } from "kernelsu";
  import { onMount, tick } from "svelte";

  let proxyMode = $state("rule");
  let tunEnabled = $state(false);

  type LogType = "info" | "success" | "error" | "cmd";
  let logs = $state<{ time: string; msg: string; type: LogType }[]>([]);
  let logsContainer: HTMLElement;
  let theme = $state<"system" | "light" | "dark">("system");

  function addLog(msg: string, type: LogType = "info") {
    if (!msg) return;
    const time = new Date().toLocaleTimeString("en-US", { hour12: false });
    const lines = msg.split("\n").filter((line) => line.trim() !== "");
    lines.forEach((line) => {
      logs.push({ time, msg: line, type });
    });
    if (logs.length > 200) logs = logs.slice(logs.length - 200);
    tick().then(() => {
      if (logsContainer) {
        logsContainer.scrollTop = logsContainer.scrollHeight;
      }
    });
  }

  async function execute(actionCmd: string, desc: string, showLog: boolean = true) {
    if (showLog) addLog(`> ${desc}`, "cmd");
    try {
      const result = await exec(`su -c '/data/adb/modules/mono_box/action.sh ${actionCmd}'`, { cwd: "/data/adb/modules/mono_box" });
      const raw = [result.stdout, result.stderr].filter(Boolean).join("\n").trim();
      const isError = result.errno !== 0;

      let json = null;
      let output = raw;
      try {
        // Attempt to extract JSON from mixed output (e.g. during upgrade_core)
        const lines = raw.split("\n");
        const jsonLine = lines.find((l) => l.trim().startsWith("{") && l.trim().endsWith("}"));
        if (jsonLine) {
          json = JSON.parse(jsonLine);
          output = raw.replace(jsonLine, "").trim();
        }
      } catch (e) {}

      return { success: !isError, errno: result.errno, raw, output, json };
    } catch (e: any) {
      const errMsg = e.message || String(e);
      if (showLog) addLog(errMsg, "error");
      return { success: false, errno: -1, raw: errMsg, output: errMsg, json: null };
    }
  }

  async function fetchStatus() {
    const res = await execute("get_configs", "get_configs", false);
    if (res.success && res.raw) {
      try {
        const config = JSON.parse(res.raw);
        if (config.mode) proxyMode = config.mode;
        if (config.tun && typeof config.tun.enable === "boolean") {
          tunEnabled = config.tun.enable;
        }
      } catch (err) {}
    }
  }

  async function switchTunMode(enable: boolean) {
    const value = enable ? "true" : "false";
    const res = await execute(`switch_tun ${value}`, `switch_tun ${value}`);
    if (res.success) {
      addLog(res.output || `TUN mode switched to ${value}`, "success");
      tunEnabled = enable;
    } else {
      addLog(`Error (${res.errno}): ${res.raw}`, "error");
    }
    await fetchStatus();
  }

  async function switchProxyMode(newMode: string) {
    const res = await execute(`switch_mode ${newMode}`, `switch_mode ${newMode}`);
    if (res.success) {
      addLog(res.output || `Proxy mode switched to ${newMode}`, "success");
      proxyMode = newMode;
    } else {
      addLog(`Error (${res.errno}): ${res.raw}`, "error");
    }
    await fetchStatus();
  }

  async function runAction(action: string) {
    const res = await execute(action, action);

    if (res.success) {
      if (action === "upgrade_core") {
        if (res.json) {
          if (res.json.status === "ok") {
            addLog(`更新成功！ (Update OK)`, "success");
            if (res.output) addLog(res.output, "info");
          } else if (res.json.message) {
            addLog(`更新提示: ${res.json.message}`, "info");
            if (res.output) addLog(res.output, "info");
          } else {
            addLog(res.raw, "info");
          }
        } else {
          addLog(res.raw || "Action executed successfully", "success");
        }
      } else {
        addLog(res.raw || "Action executed successfully", "success");
      }
    } else {
      addLog(`Error (${res.errno}): ${res.raw}`, "error");
    }

    if (["start", "restart", "upgrade_core"].includes(action)) {
      setTimeout(fetchStatus, 2000);
    }
  }

  function applyTheme(t: "system" | "light" | "dark") {
    const isDark = t === "dark" || (t === "system" && window.matchMedia("(prefers-color-scheme: dark)").matches);
    if (isDark) {
      document.documentElement.classList.add("dark");
    } else {
      document.documentElement.classList.remove("dark");
    }
  }

  function setTheme(t: "system" | "light" | "dark") {
    theme = t;
    localStorage.setItem("theme", t);
    applyTheme(t);
  }

  onMount(async () => {
    try {
      const savedTheme = localStorage.getItem("theme") as "system" | "light" | "dark";
      if (savedTheme && ["system", "light", "dark"].includes(savedTheme)) {
        theme = savedTheme;
      }
      applyTheme(theme);

      window.matchMedia("(prefers-color-scheme: dark)").addEventListener("change", () => {
        if (theme === "system") applyTheme("system");
      });

      if (typeof enableEdgeToEdge === "function") {
        enableEdgeToEdge(true);
      }
      addLog("Initializing WebUI...", "info");
      const res = await execute("status", "status", false);
      if (res.raw) addLog(res.raw, res.success ? "info" : "error");
      await fetchStatus();
    } catch (e) {}
  });
</script>

<nav
  class="fixed top-0 left-0 w-full bg-white dark:bg-zinc-950 border-b border-slate-300 dark:border-zinc-800 flex items-center justify-between px-4 z-50 shadow-sm transition-colors"
  style="padding-top: env(safe-area-inset-top); height: calc(3.5rem + env(safe-area-inset-top));"
>
  <div class="font-bold text-lg text-slate-900 dark:text-slate-100 uppercase tracking-widest">Mono Box</div>
  <div class="flex bg-slate-100 dark:bg-zinc-900 border border-slate-300 dark:border-zinc-700 text-xs font-bold uppercase tracking-wider">
    <button
      class="px-3 py-1.5 transition-colors {theme === 'light'
        ? 'bg-slate-800 text-white dark:bg-slate-200 dark:text-zinc-900'
        : 'text-slate-500 hover:text-slate-800 dark:text-slate-400 dark:hover:text-slate-200'}"
      onclick={() => setTheme("light")}>浅色</button
    >
    <button
      class="px-3 py-1.5 transition-colors border-l border-r border-slate-300 dark:border-zinc-700 {theme === 'system'
        ? 'bg-slate-800 text-white dark:bg-slate-200 dark:text-zinc-900'
        : 'text-slate-500 hover:text-slate-800 dark:text-slate-400 dark:hover:text-slate-200'}"
      onclick={() => setTheme("system")}>系统</button
    >
    <button
      class="px-3 py-1.5 transition-colors {theme === 'dark'
        ? 'bg-slate-800 text-white dark:bg-slate-200 dark:text-zinc-900'
        : 'text-slate-500 hover:text-slate-800 dark:text-slate-400 dark:hover:text-slate-200'}"
      onclick={() => setTheme("dark")}>深色</button
    >
  </div>
</nav>

<main class="max-w-3xl mx-auto px-4 pb-16 min-h-screen flex flex-col gap-6" style="padding-top: calc(5rem + env(safe-area-inset-top));">
  <!-- Core / Tun Card -->
  <section class="bg-white dark:bg-zinc-900 border border-slate-300 dark:border-zinc-700 transition-colors">
    <div class="px-4 py-3 border-b border-slate-300 dark:border-zinc-700 bg-slate-50 dark:bg-zinc-950">
      <h2 class="text-xs font-bold uppercase tracking-widest text-slate-600 dark:text-zinc-400 m-0">网络接管与内核</h2>
    </div>
    <div class="p-4 flex flex-col gap-4">
      <div class="flex items-center justify-between">
        <div class="flex flex-col">
          <span class="text-sm font-bold text-slate-900 dark:text-slate-200">TUN 模式开启</span>
          <span class="text-xs text-slate-500 dark:text-slate-400 mt-1">{tunEnabled ? "底层流量网络全局接管中" : "已停用底层接管"}</span>
        </div>
        <div class="flex border border-slate-300 dark:border-zinc-700 font-bold text-sm">
          <button
            class="px-4 py-1.5 transition-colors {tunEnabled ? 'bg-slate-800 text-white dark:bg-slate-200 dark:text-zinc-900' : 'text-slate-500 hover:bg-slate-100 dark:hover:bg-zinc-800'}"
            onclick={() => switchTunMode(true)}>ON</button
          >
          <button
            class="px-4 py-1.5 transition-colors {!tunEnabled ? 'bg-slate-800 text-white dark:bg-slate-200 dark:text-zinc-900' : 'text-slate-500 hover:bg-slate-100 dark:hover:bg-zinc-800'}"
            onclick={() => switchTunMode(false)}>OFF</button
          >
        </div>
      </div>
      <div class="h-px bg-slate-200 dark:bg-zinc-800 w-full"></div>
      <div class="flex items-center justify-between">
        <div class="flex flex-col">
          <span class="text-sm font-bold text-slate-900 dark:text-slate-200">内核在线更新</span>
          <span class="text-xs text-slate-500 dark:text-slate-400 mt-1">调用 Mihomo Core 内置 API 检查更新</span>
        </div>
        <button
          class="border border-slate-300 dark:border-zinc-700 px-4 py-1.5 text-sm font-bold hover:bg-slate-100 dark:hover:bg-zinc-800 transition-colors text-slate-800 dark:text-slate-200 active:bg-slate-200"
          onclick={() => runAction("upgrade_core")}>立即拉取</button
        >
      </div>
    </div>
  </section>

  <!-- Proxy Mode -->
  <section class="bg-white dark:bg-zinc-900 border border-slate-300 dark:border-zinc-700 transition-colors">
    <div class="px-4 py-3 border-b border-slate-300 dark:border-zinc-700 bg-slate-50 dark:bg-zinc-950">
      <h2 class="text-xs font-bold uppercase tracking-widest text-slate-600 dark:text-zinc-400 m-0">代理模式选择</h2>
    </div>
    <div class="p-4">
      <div class="flex border border-slate-300 dark:border-zinc-700 text-sm font-bold w-full text-center">
        {#each ["rule", "global", "direct"] as mode}
          <button
            class="flex-1 py-2.5 transition-colors border-r last:border-r-0 border-slate-300 dark:border-zinc-700 {proxyMode === mode
              ? 'bg-slate-800 text-white dark:bg-slate-200 dark:text-zinc-900'
              : 'text-slate-500 hover:bg-slate-100 dark:hover:bg-zinc-800'}"
            onclick={() => switchProxyMode(mode)}
          >
            {mode.toUpperCase()}
          </button>
        {/each}
      </div>
    </div>
  </section>

  <!-- Services Actions -->
  <section class="bg-white dark:bg-zinc-900 border border-slate-300 dark:border-zinc-700 transition-colors">
    <div class="px-4 py-3 border-b border-slate-300 dark:border-zinc-700 bg-slate-50 dark:bg-zinc-950">
      <h2 class="text-xs font-bold uppercase tracking-widest text-slate-600 dark:text-zinc-400 m-0">快速控制指令</h2>
    </div>
    <div class="p-4 grid grid-cols-2 sm:grid-cols-4 gap-3">
      {#each [{ id: "start", label: "启动" }, { id: "stop", label: "停止" }, { id: "restart", label: "重启" }, { id: "status", label: "状态" }] as action}
        <button
          class="border border-slate-300 dark:border-zinc-700 py-3 text-sm font-bold text-slate-800 dark:text-slate-200 hover:bg-slate-100 dark:hover:bg-zinc-800 active:bg-slate-200 dark:active:bg-zinc-700 transition-colors outline-none"
          onclick={() => runAction(action.id)}
        >
          {action.label}
        </button>
      {/each}
    </div>
  </section>

  <!-- Log Terminal -->
  <section class="bg-zinc-950 border border-zinc-800 flex flex-col h-100 w-full mt-4">
    <div class="px-4 py-2 bg-zinc-900 border-b border-zinc-800 flex justify-between items-center shrink-0">
      <h2 class="m-0 text-xs font-mono font-bold tracking-widest text-zinc-500 uppercase">Terminal Logs</h2>
      <button class="text-xs font-mono text-zinc-500 hover:text-zinc-300 transition-colors outline-none" onclick={() => (logs = [])}>CLEAR</button>
    </div>
    <div class="p-4 flex-1 overflow-y-auto font-mono text-xs leading-relaxed custom-scrollbar bg-black" bind:this={logsContainer}>
      {#if logs.length === 0}
        <div class="text-zinc-700 text-base text-center mt-10">Waiting for output...</div>
      {:else}
        {#each logs as log}
          <div class="flex gap-4 mb-1 border-b border-zinc-900 pb-1 break-all">
            <span class="text-zinc-600 shrink-0 select-none">[{log.time}]</span>
            <span
              class={log.type === "error"
                ? "text-red-400 whitespace-pre-wrap"
                : log.type === "cmd"
                  ? "text-sky-400 font-bold whitespace-pre-wrap"
                  : log.type === "success"
                    ? "text-green-400 whitespace-pre-wrap"
                    : "text-zinc-300 whitespace-pre-wrap"}>{log.msg}</span
            >
          </div>
        {/each}
      {/if}
    </div>
  </section>
</main>

<style>
  /* Custom thin scrollbar for log box */
  .custom-scrollbar::-webkit-scrollbar {
    width: 6px;
    height: 6px;
  }
  .custom-scrollbar::-webkit-scrollbar-track {
    background: transparent;
  }
  .custom-scrollbar::-webkit-scrollbar-thumb {
    background: #334155;
    border-radius: 10px;
  }
  .custom-scrollbar::-webkit-scrollbar-thumb:hover {
    background: #475569;
  }
</style>
