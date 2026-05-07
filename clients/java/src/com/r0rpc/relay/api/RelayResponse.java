package com.r0rpc.relay.api;

import com.r0rpc.client.Base64Util;

import java.io.IOException;
import java.io.PrintWriter;
import java.io.StringWriter;
import java.lang.reflect.Array;
import java.util.ArrayList;
import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.concurrent.atomic.AtomicBoolean;

public final class RelayResponse {
    public interface ResultSender {
        void send(String requestId, String status, int httpCode, Map<String, Object> payload, String error, long latencyMs) throws IOException;
    }

    private final String requestId;
    private final ResultSender resultSender;
    private final long startedAt;
    private final AtomicBoolean responded = new AtomicBoolean(false);

    public RelayResponse(String requestId, ResultSender resultSender, long startedAt) {
        this.requestId = requestId;
        this.resultSender = resultSender;
        this.startedAt = startedAt;
    }

    public boolean success() { return success(Collections.<String, Object>emptyMap()); }
    public boolean success(Object data) { return sendOnce("success", 200, toPayload(data), ""); }
    public boolean failed(String errorMessage) { return failed(500, errorMessage); }
    public boolean failed(int httpCode, String errorMessage) {
        return sendOnce("error", httpCode <= 0 ? 500 : httpCode, Collections.<String, Object>emptyMap(), errorMessage == null ? "" : errorMessage);
    }
    public boolean failed(Throwable throwable) { return failed(500, stackTraceOf(throwable)); }
    public boolean isResponded() { return responded.get(); }

    private boolean sendOnce(String status, int httpCode, Map<String, Object> payload, String error) {
        if (!responded.compareAndSet(false, true)) { return false; }
        try {
            resultSender.send(requestId, status, httpCode, payload, error, System.currentTimeMillis() - startedAt);
            return true;
        } catch (IOException ex) {
            throw new IllegalStateException("send response failed", ex);
        }
    }

    @SuppressWarnings("unchecked")
    private static Map<String, Object> toPayload(Object data) {
        if (data == null) { return Collections.<String, Object>emptyMap(); }
        if (data instanceof Map) { return (Map<String, Object>) data; }
        Map<String, Object> payload = new LinkedHashMap<String, Object>();
        payload.put("data", normalizeValue(data));
        return payload;
    }

    private static Object normalizeValue(Object value) {
        if (value == null || value instanceof String || value instanceof Number || value instanceof Boolean) { return value; }
        if (value instanceof byte[]) { return Base64Util.encode((byte[]) value); }
        if (value instanceof Map) {
            Map<?, ?> input = (Map<?, ?>) value;
            Map<String, Object> output = new LinkedHashMap<String, Object>();
            for (Map.Entry<?, ?> entry : input.entrySet()) { output.put(String.valueOf(entry.getKey()), normalizeValue(entry.getValue())); }
            return output;
        }
        if (value instanceof Iterable) {
            List<Object> output = new ArrayList<Object>();
            for (Object item : (Iterable<?>) value) { output.add(normalizeValue(item)); }
            return output;
        }
        if (value.getClass().isArray()) {
            List<Object> output = new ArrayList<Object>();
            int length = Array.getLength(value);
            for (int i = 0; i < length; i++) { output.add(normalizeValue(Array.get(value, i))); }
            return output;
        }
        return String.valueOf(value);
    }

    private static String stackTraceOf(Throwable throwable) {
        if (throwable == null) { return ""; }
        StringWriter stringWriter = new StringWriter();
        PrintWriter printWriter = new PrintWriter(stringWriter);
        throwable.printStackTrace(printWriter);
        printWriter.flush();
        return stringWriter.toString();
    }
}