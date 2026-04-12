const COLUMNS = [
    "hook_name", "event_time", "session_id", "run_id", "agent_id",
    "deployment_id", "deployment_name", "session_key", "tool_name",
    "provider", "model", "hook_decision", "block_reason", "event_data", "agent_type",
];
export async function send(event, config) {
    const url = `${config.timeplusUrl}/proton/v1/ingest/streams/${config.stream}`;
    const authHeader = `Basic ${btoa(`${config.username}:${config.password}`)}`;
    const body = JSON.stringify({
        columns: COLUMNS,
        data: [COLUMNS.map((col) => event[col])],
    });
    try {
        const resp = await fetch(url, {
            method: "POST",
            headers: {
                "Content-Type": "application/json",
                "Authorization": authHeader,
            },
            body,
        });
        if (!resp.ok) {
            process.stderr.write(`[agentshield] ingest failed: ${resp.status}\n`);
        }
    }
    catch (err) {
        process.stderr.write(`[agentshield] network error: ${String(err)}\n`);
    }
}
//# sourceMappingURL=timeplus-client.js.map