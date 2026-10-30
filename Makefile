.DEFAULT_GOAL := build
.NOTPARALLEL:
.PHONY: default build prerelease webui native check clean

ARCH ?= arm64
WEBUI_DIR ?=
WEBUI_ARG = $(if $(strip $(WEBUI_DIR)),--webui-dir "$(WEBUI_DIR)")

default: build

build:
	sh build.sh --arch "$(ARCH)" $(WEBUI_ARG)

prerelease:
	sh build.sh --prerelease --arch "$(ARCH)" $(WEBUI_ARG)

webui:
	sh build.sh --webui-only

native:
	sh build.sh --native-only --arch "$(ARCH)"

check:
	sh build.sh --check

clean:
	sh build.sh --clean
