PYTHON  ?= python3.12
PYPIRC  ?= .pypirc

.PHONY: test build check publish-test publish clean help

## test: run the SDK unit tests (needs the dev extras: pip install -e '.[dev]')
test:
	$(PYTHON) -m pytest

## build: build sdist + wheel into dist/ from a clean dist/ (needs hatch)
build: clean
	$(PYTHON) -m hatch build

## check: validate the built distributions' metadata and README rendering (needs twine)
check: build
	$(PYTHON) -m twine check --strict dist/*

## publish-test: upload to TestPyPI using the [testpypi] section of .pypirc
publish-test: check
	$(PYTHON) -m twine upload --config-file $(PYPIRC) --repository testpypi dist/*

## publish: upload to PyPI using the [pypi] section of .pypirc
publish: check
	$(PYTHON) -m twine upload --config-file $(PYPIRC) dist/*

## clean: remove build artifacts
clean:
	rm -rf dist/ *.egg-info src/*.egg-info

## help: list targets
help:
	@grep -E '^## ' Makefile | sed 's/## /  /'
