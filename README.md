# Mono Box

[English](README.md) | [中文](README_zh.md)

> [!WARNING]
> PERSONAL-USE MODIFICATION. This repository is mainly for my own daily usage. Behavior, defaults, and compatibility may change at any time without backward-compatibility guarantees.

> [!IMPORTANT]
> This module supports only Mihomo and Mihomo Smart cores, and runs in bare-core mode only.
> This project does not provide a transparent proxy framework; it uses Mihomo native TUN capability.

## Overview

Mono Box is a Magisk / KernelSU / APatch module for Android, focused on directly running Mihomo core.

Current scope:

- Supported cores: [Mihomo](https://github.com/MetaCubeX/mihomo), [Mihomo Smart](https://github.com/vernesong/mihomo/releases/tag/Prerelease-Alpha)
- Runtime model: bare-core only (start, stop, restart, status)
- Optional network-event linkage through Clash API mode/selector switching

## WEB UI Support

Mono Box provides WEB UI management panel for convenient control. Key features include:

- **Network Takeover & Core Control**
  - Toggle TUN mode (on/off), view current network takeover status
  - Online update of Mihomo core via built-in API
  - Quick access to core panel
- **Proxy Mode Selection**
  - Switch between RULE, GLOBAL, DIRECT modes
- **Real-time Core Information**
  - Real-time memory usage, upload/download speed, total traffic
- **Core Status & Ports**
  - View core ports and core version
- **Quick Control Actions**
  - Start, Stop, Restart, Status for the core
- **box.config Configuration**
  - Edit Clash API port, secret, and toggle service behavior
- **WEB UI Settings**
  - Enable edge-to-edge immersive mode
  - Customize core panel jump URL
  - Configure homepage module display and order
- **Terminal Logs**
  - View real-time logs and service output

## Install

1. Download the latest module from Releases:
   https://github.com/LufsX/mono-box/releases
2. Install ZIP via Magisk Manager, KernelSU Manager, or APatch Manager.
3. The module supports online update metadata.

Package naming:

- MonoBox_vX.Y.zip

## Core Placement

This repository does not ship third-party core binaries. You may download during install or place one manually.

If you place one manually, put executable `mihomo` into:

- /data/adb/box/bin/

## Runtime Structure

Main paths:

- /data/adb/box/
- /data/adb/box/scripts/box.config
- /data/adb/box/scripts/box.service
- /data/adb/box/scripts/box.controller
- /data/adb/box/run/

Service entry:

- /data/adb/service.d/box_service.sh

## Key Configuration

File:

- box/scripts/box.config

Important fields:

- bin_name: core name, recommended Mihomo or Mihomo Smart
- CORE_USER_GROUP: user:group for launching core process
- clash_api_port, clash_api_secret: Clash API access
- ctr_mode: disable | switch | selector | mode
- select_outbound / default_outbound / direct_outbound
- default_clash_mode / direct_clash_mode

Notes:

- switch/selector/mode rely on Clash API exposed by your running core config.
- If Clash API is disabled in core config, policy linkage features will not work.

## Usage

Default behavior:

- On boot, module starts core unless manual mode is enabled or module is disabled.

Manager control:

- Enable module to run service
- Disable module to stop service

Action control:

- Use Manager or run service script manually for temporary core start/stop control

Manual mode:

1. Create empty file /data/adb/box/manual
2. Use:

- /data/adb/box/scripts/box.service start
- /data/adb/box/scripts/box.service stop
- /data/adb/box/scripts/box.service restart
- /data/adb/box/scripts/box.service status

Or:

- /data/adb/modules/mono_box/action.sh start
- /data/adb/modules/mono_box/action.sh stop
- /data/adb/modules/mono_box/action.sh restart
- /data/adb/modules/mono_box/action.sh status

## Uninstall

- Uninstall module from Magisk / KernelSU / APatch manager.
- /data/adb/service.d/box_service.sh will be removed by uninstall script.
- /data/adb/box data is kept by default.

To remove data manually:

- rm -rf /data/adb/box

# TODO

- [x] WEB UI Management Panel
- [x] Clash API Switch to TUN Mode
- [x] Action.sh Supports Custom Execution Actions in WEB UI
