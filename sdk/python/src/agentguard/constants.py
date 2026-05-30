AGENTGUARD_TRACER_NAME = "agentguard-sdk"


class ResourceAttribute:
    AGENT_ID = "agent.id"
    DEPLOYMENT_ID = "deployment.id"
    DEPLOYMENT_NAME = "deployment.name"


class EnvName:
    AGENT_ID = "AGENTGUARD_AGENT_ID"
    DEPLOYMENT_ID = "AGENTGUARD_DEPLOYMENT_ID"
    DEPLOYMENT_NAME = "AGENTGUARD_DEPLOYMENT_NAME"


# OpenTelemetry GenAI semantic conventions
class GenAIAttribute:
    SYSTEM = "gen_ai.system"
    OPERATION_NAME = "gen_ai.operation.name"
    REQUEST_MODEL = "gen_ai.request.model"
    RESPONSE_MODEL = "gen_ai.response.model"
    USAGE_INPUT_TOKENS = "gen_ai.usage.input_tokens"
    USAGE_OUTPUT_TOKENS = "gen_ai.usage.output_tokens"
    USAGE_TOTAL_TOKENS = "gen_ai.usage.total_tokens"
    TOKEN_TYPE = "gen_ai.token.type"
    RESPONSE_FINISH_REASON = "gen_ai.response.finish_reasons"
    REQUEST_TEMPERATURE = "gen_ai.request.temperature"
    REQUEST_TOP_P = "gen_ai.request.top_p"
    REQUEST_MAX_TOKENS = "gen_ai.request.max_tokens"
    REQUEST_FREQUENCY_PENALTY = "gen_ai.request.frequency_penalty"
    REQUEST_PRESENCE_PENALTY = "gen_ai.request.presence_penalty"
    INPUT = "gen_ai.prompt"
    OUTPUT = "gen_ai.completion"


# AgentGuard-specific span attributes
class AgentGuardAttribute:
    OBSERVATION_TYPE = "agentguard.observation_type"
    SESSION_ID = "agentguard.session_id"
    RUN_ID = "agentguard.run_id"
    USER_ID = "agentguard.user_id"
    COST_INPUT = "agentguard.cost.input"
    COST_OUTPUT = "agentguard.cost.output"
    COST_TOTAL = "agentguard.cost.total"
    CACHE_READ_TOKENS = "agentguard.usage.cache_read_tokens"
    CACHE_WRITE_TOKENS = "agentguard.usage.cache_write_tokens"
