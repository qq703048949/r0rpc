package com.r0rpc.client;

import java.util.Collections;
import java.util.Map;

public final class RpcResponse {
    private final String status;
    private final int httpCode;
    private final Map<String, Object> payload;
    private final String error;

    private RpcResponse(String status, int httpCode, Map<String, Object> payload, String error) {
        this.status = status;
        this.httpCode = httpCode;
        this.payload = payload == null ? Collections.<String, Object>emptyMap() : payload;
        this.error = error == null ? "" : error;
    }

    public static RpcResponse success(Map<String, Object> payload) {
        return new RpcResponse("success", 200, payload, "");
    }

    public static RpcResponse error(String errorMessage) {
        return new RpcResponse("error", 500, Collections.<String, Object>emptyMap(), errorMessage);
    }

    public String getStatus() {
        return status;
    }

    public int getHttpCode() {
        return httpCode;
    }

    public Map<String, Object> getPayload() {
        return payload;
    }

    public String getError() {
        return error;
    }
}
