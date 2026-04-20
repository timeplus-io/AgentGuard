PYTHON  := python3.12
PYPIRC  := .pypirc

.PHONY: build publish install clean

## Build sdist + wheel into dist/
build:
	$(PYTHON) -m hatch build

## Upload to PyPI using .pypirc credentials
publish: build
	$(PYTHON) -m pip install twine --break-system-packages --quiet
	$(PYTHON) -m twine upload --config-file $(PYPIRC) dist/*

## Install the plugin into ~/.hermes/plugins/agentguard/ from local source
install:
	$(PYTHON) -m pip install -e . --break-system-packages --quiet
	agentguard-hermes install

## Remove build artifacts
clean:
	rm -rf dist/ *.egg-info agentguard_hermes.egg-info
