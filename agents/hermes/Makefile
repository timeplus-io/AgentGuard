PWD         := $(shell pwd)
HERMES_DATA := $(PWD)/.hermes
HERMES_IMAGE := nousresearch/hermes-agent:latest

export AGENTGUARD_AGENT_ID        ?= $(shell hostname)
export AGENTGUARD_DEPLOYMENT_ID   ?= local
export AGENTGUARD_DEPLOYMENT_NAME ?= Local Dev
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
	cp agentguard-plugin/plugin.py  $(HERMES_DATA)/plugins/agentguard/plugin.py
	@echo "AgentGuard gateway hook → $(HERMES_DATA)/hooks/agentguard/"
	@echo "AgentGuard plugin       → $(HERMES_DATA)/plugins/agentguard/"
	@python3 -c "import yaml; path='$(HERMES_DATA)/config.yaml'; cfg=yaml.safe_load(open(path)) if __import__('os').path.exists(path) else {}; cfg.setdefault('model',{}).update({'default':'claude-sonnet-4-6','provider':'custom','base_url':'$(LITELLM_URL)'}); yaml.dump(cfg,open(path,'w'),default_flow_style=False); print('LiteLLM config → ' + path)"

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
	  -e OPENAI_API_KEY=$(LITELLM_API_KEY) \
	  $(HERMES_IMAGE)

## Full install: pull image + configure hooks (run 'make init' separately for first-time setup)
install: pull configure
