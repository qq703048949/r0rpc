package com.r0rpc.relay.api;

import java.util.Collections;
import java.util.Map;

public final class RelayRequest {
    private final String requestId;
    private final String group;
    private final String action;
    private final String clientId;
    private final Map<String, Object> payload;

    public RelayRequest(String requestId, String group, String action, String clientId, Map<String, Object> payload) {
        this.requestId = requestId == null ? "" : requestId;
        this.group = group == null ? "" : group;
        this.action = action == null ? "" : action;
        this.clientId = clientId == null ? "" : clientId;
        this.payload = payload == null ? Collections.<String, Object>emptyMap() : payload;
    }

    public String getRequestId() { return requestId; }
    public String getGroup() { return group; }
    public String getAction() { return action; }
    public String getClientId() { return clientId; }
    public Map<String, Object> getPayload() { return payload; }

    public String getString(String key) {
        Object value = payload.get(key);
        return value == null ? "" : String.valueOf(value);
    }

    public int getInt(String key, int defaultValue) {
        Object value = payload.get(key);
        if (value instanceof Number) { return ((Number) value).intValue(); }
        if (value != null) {
            try { return Integer.parseInt(String.valueOf(value)); } catch (Exception ignore) {}
        }
        return defaultValue;
    }
}