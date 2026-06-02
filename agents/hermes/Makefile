PWD         := $(shell pwd)
HERMES_DATA := $(PWD)/.hermes
HERMES_IMAGE := nousresearch/hermes-agent:latest

# Provider secrets file (NVIDIA_API_KEY, OLLAMA_API_KEY, GOOGLE_API_KEY, etc.)
# git-ignored. Loaded by `docker run --env-file` in the `cli` target and by
# `env_file:` in docker-compose.yml for `make start`. Optional — flag silently
# disappears when the file is absent, so neither path breaks.
ENV_FILE := .env
ENV_FILE_FLAG := $(if $(wildcard $(ENV_FILE)),--env-file $(ENV_FILE),)
# Also import .env into Make itself so we can reference its vars in recipes
# (e.g. to map NVIDIA_API_KEY → OPENAI_API_KEY for Hermes's `provider: custom`
# bearer-token slot). `-include` is silent when the file is absent.
# .env uses KEY=VALUE syntax which is also valid Makefile syntax.
-include $(ENV_FILE)

export AGENTGUARD_AGENT_ID        ?= $(shell hostname)
export AGENTGUARD_DEPLOYMENT_ID   ?= local-hermes
export AGENTGUARD_DEPLOYMENT_NAME ?= Local Hermes
# host.docker.internal resolves to the host from inside Docker containers
export AGENTGUARD_TIMEPLUS_URL    ?= http://host.docker.internal:3218
export AGENTGUARD_STREAM          ?= agentguard_hook_events
export AGENTGUARD_USERNAME        ?= proton
export AGENTGUARD_PASSWORD        ?= timeplus@t+
# LiteLLM proxy (agents/litellm) — OpenAI-compatible endpoint
export LITELLM_URL                ?= http://host.docker.internal:4000
export LITELLM_API_KEY            ?= sk-openclaw-proxy

.PHONY: pull workspace configure init build-web start stop logs cli clean install

## Pull the latest Hermes Docker image
pull:
	docker pull $(HERMES_IMAGE)

## Create the data directory structure expected by Hermes
workspace:
	mkdir -p $(HERMES_DATA)/{cron,sessions,logs,hooks,memories,skills,skins,plans,workspace,home}

## Install the AgentGuard gateway hook + plugin into the Hermes data directory
## - Gateway hook (handler.py): fires in gateway mode (Telegram, Discord, etc.)
## - Plugin (plugin.py):        fires in CLI mode AND gateway mode
configure: workspace
	mkdir -p $(HERMES_DATA)/hooks/agentguard
	cp agentguard-plugin/HOOK.yaml  $(HERMES_DATA)/hooks/agentguard/HOOK.yaml
	cp agentguard-plugin/handler.py $(HERMES_DATA)/hooks/agentguard/handler.py
	mkdir -p $(HERMES_DATA)/plugins/agentguard
	cp agentguard-plugin/agentguard_hermes/plugin.yaml $(HERMES_DATA)/plugins/agentguard/plugin.yaml
	cp agentguard-plugin/agentguard_hermes/plugin.py   $(HERMES_DATA)/plugins/agentguard/__init__.py
	@echo "AgentGuard gateway hook → $(HERMES_DATA)/hooks/agentguard/"
	@echo "AgentGuard plugin       → $(HERMES_DATA)/plugins/agentguard/"
	# Model selection lives in .hermes/config.yaml (model.default / .provider /
	# .base_url) and is owned by the user — set it via `hermes setup`, the
	# dashboard, or by editing config.yaml directly. Earlier versions of this
	# target rewrote model.* on every `make configure` (which `make start`
	# depends on), silently reverting any non-LiteLLM choice on every restart.
	# That was removed; configure now only manages AgentGuard hook+plugin files.

## First-time interactive setup wizard (sets API keys, model, platforms)
init: workspace
	docker run --rm -it \
	  --name hermes-setup \
	  -v "$(HERMES_DATA)":/opt/data \
	  -e HERMES_UID=$(shell id -u) \
	  $(HERMES_IMAGE) setup

## Build the web UI frontend into .hermes/web_dist (run once, or after image update)
build-web: workspace
	docker run --rm \
	  --user root \
	  -v "$(HERMES_DATA)":/opt/data \
	  --entrypoint sh \
	  $(HERMES_IMAGE) \
	  -c "cd /opt/hermes/web && npm install --silent && npm run build && cp -r /opt/hermes/hermes_cli/web_dist /opt/data/web_dist"
	@echo "Web UI built → $(HERMES_DATA)/web_dist/"

## Start gateway + web dashboard via docker compose
start: configure build-web
	docker compose up -d
	@echo "Hermes gateway running"
	@echo "Hermes web dashboard  → http://localhost:18790"

## Tail logs (all services, or pass service name: make logs s=gateway)
logs:
	docker compose logs -f $(s)

## Stop all services (data preserved)
stop:
	docker compose down

## Stop all services and wipe local data
clean:
	docker compose down
	rm -rf $(HERMES_DATA)

## Run Hermes interactively in CLI mode (AgentGuard plugin fires on every session/tool call)
cli: configure
	docker run --rm -it \
	  --name hermes-cli \
	  --add-host host.docker.internal:host-gateway \
	  -v "$(HERMES_DATA)":/opt/data \
	  -e HERMES_UID=$(shell id -u) \
	  -e AGENTGUARD_AGENT_ID=$(AGENTGUARD_AGENT_ID) \
	  -e AGENTGUARD_DEPLOYMENT_ID=$(AGENTGUARD_DEPLOYMENT_ID) \
	  -e "AGENTGUARD_DEPLOYMENT_NAME=$(AGENTGUARD_DEPLOYMENT_NAME)" \
	  -e AGENTGUARD_TIMEPLUS_URL=$(AGENTGUARD_TIMEPLUS_URL) \
	  -e AGENTGUARD_STREAM=$(AGENTGUARD_STREAM) \
	  -e AGENTGUARD_USERNAME=$(AGENTGUARD_USERNAME) \
	  -e AGENTGUARD_PASSWORD=$(AGENTGUARD_PASSWORD) \
	  -e OPENAI_API_KEY=$(or $(NVIDIA_API_KEY),$(LITELLM_API_KEY)) \
	  -e AWS_BEARER_TOKEN_BEDROCK=$(AWS_BEARER_TOKEN_BEDROCK) \
	  $(ENV_FILE_FLAG) \
	  $(HERMES_IMAGE)

## Full install: pull image + configure hooks (run 'make init' separately for first-time setup)
install: pull configure
