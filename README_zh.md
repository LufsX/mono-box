# Mono Box

[English](README.md) | [中文](README_zh.md)

> [!WARNING]
> 个人自用修改版本。该仓库以本人日常使用为主，行为、默认值和兼容性可能随时调整，不保证向后兼容。

> [!IMPORTANT]
> 本模块仅支持 Mihomo 和 Mihomo Smart 两种核心，且仅支持裸核运行。
> 当前项目不提供透明代理框架脚本，使用的是 Mihomo 内核原生的 TUN 能力。

## 项目说明

Mono Box 是一个面向 Android 的 Magisk / KernelSU / APatch 模块，专注于直接运行 Mihomo 内核。

当前范围：

- 支持核心：[Mihomo](https://github.com/MetaCubeX/mihomo)、[Mihomo Smart](https://github.com/vernesong/mihomo/releases/tag/Prerelease-Alpha)
- 运行模式：仅裸核运行（start/stop/restart/status）
- 可选能力：基于 Clash API 的联网事件联动（切换 selector 或 mode）

## WEB UI 支持

Mono Box 提供一个 WEB UI 管理面板，功能包括：

- **网络接管与内核控制**
  - TUN 模式一键开关，显示当前网络接管状态
  - 内核在线更新，调用 Mihomo 内置 API
  - 快捷跳转内核面板
- **代理模式选择**
  - 支持 RULE、GLOBAL、DIRECT 模式切换
- **实时内核信息**
  - 实时显示内存占用、上传/下载速度、累计流量
- **核心状态与端口**
  - 查看核心端口及核心版本
- **快速控制指令**
  - 启动、停止、重启、状态一键操作
- **box.config 配置**
  - 编辑 Clash API 端口、密钥，切换服务开关行为
- **WEB UI 设置**
  - 启用沉浸式全面屏（Edge-to-Edge）
  - 自定义内核面板跳转 URL
  - 首页模块显示与排序
- **Terminal Logs**
  - 实时查看日志与服务输出

## 安装

1. 从 Releases 下载最新模块：
   https://github.com/LufsX/mono-box/releases
2. 在 Magisk Manager、KernelSU Manager 或 APatch Manager 中安装 ZIP。
3. 模块支持在线更新元数据。

安装包命名：

- MonoBox_vX.Y.zip

## 核心放置

仓库不内置第三方核心二进制，可选择安装时下载或自行放置。

自行放置的话，将核心可执行文件 `mihomo` 放到 `/data/adb/box/bin/` 目录下即可。

## 运行结构

主要路径：

- /data/adb/box/
- /data/adb/box/scripts/box.config
- /data/adb/box/scripts/box.service
- /data/adb/box/scripts/box.controller
- /data/adb/box/run/

服务入口：

- /data/adb/service.d/box_service.sh

## 关键配置

配置文件：

- box/scripts/box.config

常用配置项：

- bin_name：核心名称，仅建议 Mihomo 或 Mihomo Smart
- CORE_USER_GROUP：核心运行用户组
- clash_api_port、clash_api_secret：Clash API 访问
- toggle_action：service | tun | mode_cycle
- ctr_mode：disable | switch | tun | selector | mode
- select_outbound：ctr_mode=selector 时的策略组名称
- target_cellular / target_wifi / target_wifi_list：switch、tun、selector、mode 共用的蜂窝/Wi-Fi/SSID 目标

说明：

- tun/selector/mode 联动依赖核心配置中已启用 Clash API。
- 若 Clash API 未启用，则这些联动功能不会生效。

## 使用方式

默认行为：

- 开机后模块会启动核心，除非启用了手动模式或模块被禁用。

Manager 控制：

- 启用模块：运行服务
- 禁用模块：停止服务

Action 控制：

- 通过 Manager 或手动执行服务脚本来控制核心临时的启动与停止

手动模式：

1. 创建空文件 /data/adb/box/manual
2. 手动执行：

- /data/adb/box/scripts/box.service start
- /data/adb/box/scripts/box.service stop
- /data/adb/box/scripts/box.service restart
- /data/adb/box/scripts/box.service status

或

- /data/adb/modules/mono_box/action.sh start
- /data/adb/modules/mono_box/action.sh stop
- /data/adb/modules/mono_box/action.sh restart
- /data/adb/modules/mono_box/action.sh status

## 卸载

- 在 Magisk / KernelSU / APatch 中卸载模块。
- 卸载脚本会移除 /data/adb/service.d/box_service.sh。
- 默认保留 /data/adb/box 运行数据。

如需清除数据：

- rm -rf /data/adb/box

# TODO

- [x] WEB UI 管理面板
- [x] Clash API 切换 TUN 模式
- [x] Action.sh 支持在 WEB UI 中自定义执行动作
