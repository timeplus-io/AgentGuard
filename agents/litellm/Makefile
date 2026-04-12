
PWD := $(shell pwd)

LITELLM_IMAGE := ghcr.io/berriai/litellm:main-latest
LITELLM_API_KEY := sk-openclaw-proxy
LITELLM_CONFIG := config/litellm_config.yaml
GCP_CREDENTIALS := config/tpdemo2025-9699db62024d.json
GOOGLE_CLOUD_PROJECT := tpdemo2025
GOOGLE_CLOUD_LOCATION := global

start-litellm:
	docker stop litellm-proxy || true
	docker rm litellm-proxy || true
	docker run -d \
		--name litellm-proxy \
		-p 4000:4000 \
		-v "$(PWD)/$(LITELLM_CONFIG)":/app/config.yaml:ro \
		-v "$(PWD)/$(GCP_CREDENTIALS)":/app/credentials.json:ro \
		$(LITELLM_IMAGE) \
		--config /app/config.yaml --port 4000

stop-litellm:
	docker stop litellm-proxy || true
	docker rm litellm-proxy || true