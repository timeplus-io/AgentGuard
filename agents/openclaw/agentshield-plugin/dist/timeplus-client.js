const COLUMNS = [
    "hook_name", "event_time", "session_id", "run_id", "agent_id",
    "deployment_id", "deployment_name",
    "session_key", "tool_name", "provider", "model",
    "hook_decision", "block_reason", "event_data",
];
async function postBatch(url, authHeader, batch) {
    const body = JSON.stringify({
        columns: COLUMNS,
        data: batch.map((e) => COLUMNS.map((col) => e[col])),
    });
    const resp = await fetch(url, {
        method: "POST",
        headers: {
            "Content-Type": "application/json",
            "Authorization": authHeader,
        },
        body,
    });
    if (!resp.ok) {
        throw new Error(`Timeplus ingest failed: ${resp.status}`);
    }
}
export function createTimeplusClient(config) {
    const url = `${config.timeplusUrl}/proton/v1/ingest/streams/${config.stream}`;
    const authHeader = `Basic ${btoa(`${config.username}:${config.password}`)}`;
    const queue = [];
    let timer = null;
    async function flush() {
        if (queue.length === 0)
            return;
        // Splice synchronously before the first await so any concurrent flush()
        // call that enters after this point sees an empty queue and returns early,
        // preventing the same events from being sent twice.
        const batch = queue.splice(0, queue.length);
        try {
            await postBatch(url, authHeader, batch);
        }
        catch {
            // retry once immediately, then drop the batch
            try {
                await postBatch(url, authHeader, batch);
            }
            catch (err) {
                console.error("[agentshield] Failed to flush batch, dropping:", err);
            }
        }
    }
    timer = setInterval(() => {
        void flush();
    }, config.flushMs);
    function enqueue(event) {
        queue.push(event);
        if (queue.length >= config.batchSize) {
            // Deliberate tradeoff: if a flush is already in-flight, its splice has
            // already drained the queue so this call returns early. New events will
            // be picked up on the next timer tick — acceptable for low-to-moderate volume.
            void flush();
        }
    }
    async function close() {
        if (timer !== null) {
            clearInterval(timer);
            timer = null;
        }
        await flush();
    }
    return { enqueue, flush, close };
}
//# sourceMappingURL=timeplus-client.js.map