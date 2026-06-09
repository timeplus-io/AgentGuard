import importlib
import importlib.util
import logging
from dataclasses import dataclass

logger = logging.getLogger(__name__)


@dataclass
class InstrumentorInfo:
    name: str
    instrumentor_module: str
    instrumentor_class: str
    module: str


INSTRUMENTORS: list[InstrumentorInfo] = [
    InstrumentorInfo(
        "anthropic", "openinference.instrumentation.anthropic", "AnthropicInstrumentor", "anthropic"
    ),
    InstrumentorInfo(
        "crewai", "openinference.instrumentation.crewai", "CrewAIInstrumentor", "crewai"
    ),
    InstrumentorInfo(
        "langchain", "openinference.instrumentation.langchain", "LangChainInstrumentor", "langchain"
    ),
    InstrumentorInfo(
        "openai", "openinference.instrumentation.openai", "OpenAIInstrumentor", "openai"
    ),
]


def init(
    tracer_provider,
    enabled_instrumentors: list[str],
    disabled_instrumentors: list[str],
    tracing_enabled: bool = True,
) -> None:
    """
    Initialize AgentGuard auto-instrumentation.
    """
    def _should_enable(name, module):
        if enabled_instrumentors and name not in enabled_instrumentors:
            return False

        if disabled_instrumentors and name in disabled_instrumentors:
            return False

        if importlib.util.find_spec(module) is None:
            return False

        return True

    for instrumentor in INSTRUMENTORS:
        if tracing_enabled and _should_enable(instrumentor.name, instrumentor.module):
            try:
                module = importlib.import_module(instrumentor.instrumentor_module)
                instrumentor_class = getattr(module, instrumentor.instrumentor_class)
                if instrumentor_class is not None:
                    logging.info(f"Instrumentor {instrumentor.instrumentor_class} is enabled.")
                    instrumentor_class().instrument(tracer_provider=tracer_provider)
            except Exception as e:
                logging.warning(f"Failed to enable {instrumentor.instrumentor_class}: {e}")
