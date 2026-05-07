package com.r0rpc.client;

import android.util.Log;

import com.r0rpc.relay.api.RelayHandler;
import com.r0rpc.relay.api.RelayRequest;
import com.r0rpc.relay.api.RelayResponse;
import com.r0rpc.relay.api.databind.AutoBind;

import java.io.BufferedReader;
import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.io.InputStream;
import java.io.InputStreamReader;
import java.io.OutputStream;
import java.lang.reflect.Field;
import java.lang.reflect.Modifier;
import java.net.HttpURLConnection;
import java.net.URL;
import java.nio.charset.StandardCharsets;
import java.util.LinkedHashMap;
import java.util.zip.GZIPOutputStream;
import java.util.Map;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.LinkedBlockingQueue;
import java.util.concurrent.RejectedExecutionException;
import java.util.concurrent.ThreadFactory;
import java.util.concurrent.ThreadPoolExecutor;
import java.util.concurrent.ThreadLocalRandom;
import java.util.concurrent.TimeUnit;

public class RelayClient {
    public interface ErrorHandler {
        void onError(Throwable throwable);
    }

    private static final String TAG = "R0RPC";
    private static final long BASE_RETRY_DELAY_MS = 1000L;
    private static final long MAX_RETRY_DELAY_MS = 30000L;
    private static final long HEARTBEAT_INTERVAL_MS = 5000L;
    private static final long HEARTBEAT_JITTER_MS = 1500L;
    private static final long STABLE_CONNECTION_RESET_MS = 60000L;
    private static final long SERVER_SILENCE_TIMEOUT_MS = (HEARTBEAT_INTERVAL_MS + HEARTBEAT_JITTER_MS) * 3L;
    private static final String PAYLOAD_ENCODING_GZIP_BASE64_JSON = "gzip+base64+json";
    private static final int COMPRESS_PAYLOAD_THRESHOLD_BYTES = 32 * 1024;

    private final String baseUrl;
    private final String username;
    private final String password;
    private final String clientId;
    private final String group;
    private final String platform;
    private final int connectTimeoutMs;
    private final int readTimeoutMs;
    private final Map<String, RpcHandler> handlers = new ConcurrentHashMap<String, RpcHandler>();
    private final Map<String, RelayHandler> relayHandlers = new ConcurrentHashMap<String, RelayHandler>();
    private final Object lifecycleLock = new Object();
    private final Object executorLock = new Object();

    private volatile String token;
    private volatile String wsUrl;
    private volatile long lastServerActivityAt;
    private volatile int maxInFlight = 64;
    private volatile boolean running;
    private volatile Thread workerThread;
    private volatile ErrorHandler errorHandler;
    private volatile ThreadPoolExecutor jobExecutor;

    public RelayClient(String baseUrl, String username, String password, String clientId, String group) {
        this(baseUrl, username, password, clientId, group, "android", 5000, 30000);
    }

    public RelayClient(String baseUrl, String username, String password, String clientId, String group,
                       String platform) {
        this(baseUrl, username, password, clientId, group, platform, 5000, 30000);
    }

    public RelayClient(String baseUrl, String username, String password, String clientId, String group,
                       String platform, int connectTimeoutMs, int readTimeoutMs) {
        this.baseUrl = normalizeBaseUrl(baseUrl);
        this.username = username;
        this.password = password;
        this.clientId = clientId;
        this.group = group;
        this.platform = platform;
        this.connectTimeoutMs = connectTimeoutMs;
        this.readTimeoutMs = readTimeoutMs;
    }

    public RelayClient registerAction(String action, RpcHandler handler) {
        handlers.put(action, handler);
        return this;
    }

    public RelayClient registerHandler(String action, RelayHandler handler) {
        if (handler == null) {
            throw new IllegalArgumentException("handler can not be null");
        }
        if (action == null || action.trim().isEmpty()) {
            throw new IllegalArgumentException("handler action can not be empty");
        }
        relayHandlers.put(action.trim(), handler);
        return this;
    }

    public RelayClient onError(ErrorHandler handler) {
        this.errorHandler = handler;
        return this;
    }

    public RelayClient maxInFlight(int value) {
        if (value <= 0) {
            return this;
        }
        if (value > 256) {
            value = 256;
        }
        this.maxInFlight = value;
        return this;
    }

    public RelayClient start() {
        return start("r0rpc-relay-client");
    }

    public RelayClient start(String threadName) {
        synchronized (lifecycleLock) {
            if (workerThread != null && workerThread.isAlive()) {
                return this;
            }
            final String finalThreadName = threadName == null || threadName.trim().isEmpty() ? "r0rpc-relay-client" : threadName.trim();
            running = true;
            Thread thread = new Thread(new Runnable() {
                @Override
                public void run() {
                    try {
                        loopForever();
                    } catch (Throwable throwable) {
                        notifyError(throwable);
                    } finally {
                        synchronized (lifecycleLock) {
                            if (Thread.currentThread() == workerThread) {
                                workerThread = null;
                            }
                        }
                    }
                }
            }, finalThreadName);
            thread.setDaemon(true);
            workerThread = thread;
            thread.start();
            return this;
        }
    }

    public void login() throws IOException {
        Map<String, Object> body = new LinkedHashMap<String, Object>();
        body.put("username", username);
        body.put("password", password);
        body.put("clientId", clientId);
        body.put("group", group);
        body.put("platform", platform);
        body.put("maxInFlight", Integer.valueOf(maxInFlight));

        Map<String, Object> response = postJson("/api/client/login", null, body);
        Object tokenValue = response.get("token");
        if (!(tokenValue instanceof String) || ((String) tokenValue).isEmpty()) {
            throw new IOException("Login succeeded but token is missing");
        }
        token = (String) tokenValue;
        Object maxInFlightValue = response.get("maxInFlight");
        if (maxInFlightValue instanceof Number) {
            int effective = ((Number) maxInFlightValue).intValue();
            if (effective > 0) {
                maxInFlight = effective;
            }
        }
        Object wsUrlValue = response.get("wsUrl");
        wsUrl = wsUrlValue instanceof String && !((String) wsUrlValue).isEmpty() ? (String) wsUrlValue : buildWsUrl();
        ensureJobExecutor();
    }

    public void loopForever() throws IOException {
        running = true;
        int retryAttempt = 0;

        while (running) {
            SimpleWebSocket socket = null;
            long connectedAt = 0L;
            try {
                ensureLoggedIn();
                socket = SimpleWebSocket.connect(currentWsUrl(), null, connectTimeoutMs, readTimeoutMs);
                connectedAt = System.currentTimeMillis();
                lastServerActivityAt = connectedAt;
                runSocketLoop(socket);

                if (!running) {
                    return;
                }

                if (System.currentTimeMillis() - connectedAt >= STABLE_CONNECTION_RESET_MS) {
                    retryAttempt = 0;
                }
                long delayMs = computeRetryDelayMs(retryAttempt++);
                Log.w(TAG, "relay connection closed, retry in " + delayMs + "ms, attempt=" + retryAttempt);
                sleepQuietly(delayMs);
            } catch (IOException ex) {
                if (!running) {
                    return;
                }
                if (isUnauthorized(ex)) {
                    clearSession();
                }
                if (connectedAt > 0L && System.currentTimeMillis() - connectedAt >= STABLE_CONNECTION_RESET_MS) {
                    retryAttempt = 0;
                }
                long delayMs = computeRetryDelayMs(retryAttempt++);
                Log.w(TAG, "relay reconnect scheduled in " + delayMs + "ms, attempt=" + retryAttempt + ", reason=" + safeMessage(ex));
                sleepQuietly(delayMs);
            } finally {
                closeQuietly(socket);
            }
        }
    }

    public void stop() {
        running = false;
        Thread thread = workerThread;
        if (thread != null) {
            thread.interrupt();
        }
        shutdownJobExecutor();
    }

    public Thread getWorkerThread() {
        return workerThread;
    }

    public void logoutQuietly() {
        if (token == null || token.isEmpty()) {
            return;
        }
        try {
            postJson("/api/client/logout", token, new LinkedHashMap<String, Object>());
        } catch (Exception ignore) {
        }
    }

    @SuppressWarnings("unchecked")
    private void runSocketLoop(final SimpleWebSocket socket) throws IOException {
        Thread heartbeat = new Thread(new Runnable() {
            @Override
            public void run() {
                sleepQuietly(computeHeartbeatDelayMs());
                while (running && socket.isOpen()) {
                    if (isServerSilent()) {
                        Log.w(TAG, "relay server silent, closing socket for reconnect");
                        closeQuietly(socket);
                        return;
                    }
                    try {
                        socket.sendHeartbeat();
                    } catch (IOException ex) {
                        closeQuietly(socket);
                        return;
                    }
                    sleepQuietly(computeHeartbeatDelayMs());
                }
            }
        }, "r0rpc-heartbeat");
        heartbeat.setDaemon(true);
        heartbeat.start();

        while (running && socket.isOpen()) {
            String text = socket.readText();
            if (text == null || text.isEmpty()) {
                return;
            }
            lastServerActivityAt = System.currentTimeMillis();
            Object parsed = MiniJson.parse(text);
            if (!(parsed instanceof Map)) {
                continue;
            }
            Map<String, Object> message = (Map<String, Object>) parsed;
            String type = asString(message.get("type"));
            if (!"job".equals(type)) {
                continue;
            }
            Object jobObject = message.get("job");
            if (!(jobObject instanceof Map)) {
                continue;
            }
            dispatchJob(socket, (Map<String, Object>) jobObject);
        }
    }

    @SuppressWarnings("unchecked")
    private void dispatchJob(final SimpleWebSocket socket, Map<String, Object> job) throws IOException {
        ensureJobExecutor();
        final Map<String, Object> jobCopy = new LinkedHashMap<String, Object>(job);
        Object payload = jobCopy.get("payload");
        if (payload instanceof Map) {
            jobCopy.put("payload", new LinkedHashMap<String, Object>((Map<String, Object>) payload));
        }
        try {
            jobExecutor.execute(new Runnable() {
                @Override
                public void run() {
                    try {
                        handleJob(socket, jobCopy);
                    } catch (IOException ex) {
                        closeQuietly(socket);
                        notifyError(ex);
                    }
                }
            });
        } catch (RejectedExecutionException ex) {
            throw new IOException("client worker pool is full", ex);
        }
    }

    @SuppressWarnings("unchecked")
    private void handleJob(final SimpleWebSocket socket, Map<String, Object> job) throws IOException {
        String requestId = asString(job.get("requestId"));
        String action = asString(job.get("action"));
        String groupName = asString(job.get("group"));
        String targetClientId = asString(job.get("clientId"));
        Map<String, Object> payload = job.get("payload") instanceof Map ? (Map<String, Object>) job.get("payload") : new LinkedHashMap<String, Object>();

        long startedAt = System.currentTimeMillis();
        RelayRequest request = new RelayRequest(requestId, groupName, action, targetClientId, payload);
        RelayResponse response = new RelayResponse(requestId, new RelayResponse.ResultSender() {
            @Override
            public void send(String respondedRequestId, String status, int httpCode, Map<String, Object> respondedPayload, String error, long latencyMs) throws IOException {
                sendResult(socket, respondedRequestId, status, httpCode, respondedPayload, error, latencyMs);
            }
        }, startedAt);

        RelayHandler relayHandler = relayHandlers.get(action);
        if (relayHandler != null) {
            try {
                invokeRelayHandler(relayHandler, request, response);
            } catch (Throwable throwable) {
                response.failed(throwable);
            }
            return;
        }

        RpcHandler handler = handlers.get(action);
        if (handler == null) {
            response.failed("No handler registered for action: " + action);
            return;
        }

        try {
            RpcResponse rpcResponse = handler.handle(payload);
            if (rpcResponse == null) {
                response.success();
                return;
            }
            if ("success".equalsIgnoreCase(rpcResponse.getStatus())) {
                response.success(rpcResponse.getPayload());
            } else {
                response.failed(rpcResponse.getHttpCode(), rpcResponse.getError());
            }
        } catch (Throwable throwable) {
            response.failed(throwable);
        }
    }

    private void invokeRelayHandler(RelayHandler handler, RelayRequest request, RelayResponse response) throws Exception {
        RelayHandler effectiveHandler = createRequestScopedRelayHandler(handler);
        if (effectiveHandler != handler) {
            bindAutoFields(effectiveHandler, request);
            effectiveHandler.handleRequest(request, response);
            return;
        }
        if (requiresSerializedRelayHandler(handler)) {
            synchronized (handler) {
                bindAutoFields(handler, request);
                handler.handleRequest(request, response);
            }
            return;
        }
        bindAutoFields(handler, request);
        handler.handleRequest(request, response);
    }

    private void bindAutoFields(RelayHandler handler, RelayRequest request) throws IllegalAccessException {
        Class<?> current = handler.getClass();
        while (current != null && current != Object.class) {
            Field[] fields = current.getDeclaredFields();
            for (Field field : fields) {
                AutoBind autoBind = field.getAnnotation(AutoBind.class);
                if (autoBind == null || Modifier.isStatic(field.getModifiers())) {
                    continue;
                }
                String key = autoBind.key().trim().isEmpty() ? field.getName() : autoBind.key().trim();
                Object raw = request.getPayload().get(key);
                Object converted = convertValue(field.getType(), raw, autoBind);
                field.setAccessible(true);
                field.set(handler, converted);
            }
            current = current.getSuperclass();
        }
    }

    private Object convertValue(Class<?> type, Object raw, AutoBind autoBind) {
        if (type == String.class) {
            return raw == null ? autoBind.defaultStringValue() : String.valueOf(raw);
        }
        if (type == int.class || type == Integer.class) {
            if (raw instanceof Number) { return Integer.valueOf(((Number) raw).intValue()); }
            if (raw != null) {
                try { return Integer.valueOf(Integer.parseInt(String.valueOf(raw))); } catch (Exception ignore) {}
            }
            return Integer.valueOf(autoBind.defaultIntValue());
        }
        if (type == long.class || type == Long.class) {
            if (raw instanceof Number) { return Long.valueOf(((Number) raw).longValue()); }
            if (raw != null) {
                try { return Long.valueOf(Long.parseLong(String.valueOf(raw))); } catch (Exception ignore) {}
            }
            return Long.valueOf(autoBind.defaultLongValue());
        }
        if (type == boolean.class || type == Boolean.class) {
            if (raw instanceof Boolean) { return raw; }
            if (raw != null) { return Boolean.valueOf(Boolean.parseBoolean(String.valueOf(raw))); }
            return Boolean.valueOf(autoBind.defaultBooleanValue());
        }
        if (Map.class.isAssignableFrom(type)) {
            return raw instanceof Map ? raw : new LinkedHashMap<String, Object>();
        }
        return raw;
    }

    private void sendResult(SimpleWebSocket socket, String requestId, String status, int httpCode, Map<String, Object> payload, String error, long latencyMs) throws IOException {
        EncodedPayload encodedPayload = encodePayload(payload == null ? new LinkedHashMap<String, Object>() : payload);

        Map<String, Object> resultBody = new LinkedHashMap<String, Object>();
        resultBody.put("requestId", requestId);
        resultBody.put("status", status);
        resultBody.put("httpCode", httpCode);
        resultBody.put("payload", encodedPayload.payload);
        if (encodedPayload.encoding.length() > 0) {
            resultBody.put("payloadEncoding", encodedPayload.encoding);
            resultBody.put("payloadRawSize", Integer.valueOf(encodedPayload.rawSize));
            resultBody.put("payloadCompressedSize", Integer.valueOf(encodedPayload.compressedSize));
        }
        resultBody.put("error", error == null ? "" : error);
        resultBody.put("latencyMs", latencyMs);

        Map<String, Object> envelope = new LinkedHashMap<String, Object>();
        envelope.put("type", "result");
        envelope.put("result", resultBody);
        socket.sendText(MiniJson.stringify(envelope));
    }

    @SuppressWarnings("unchecked")
    private Map<String, Object> postJson(String path, String bearerToken, Map<String, Object> body) throws IOException {
        HttpURLConnection connection = null;
        try {
            URL url = new URL(baseUrl + path);
            connection = (HttpURLConnection) url.openConnection();
            connection.setRequestMethod("POST");
            connection.setDoOutput(true);
            connection.setConnectTimeout(connectTimeoutMs);
            connection.setReadTimeout(readTimeoutMs);
            connection.setRequestProperty("Content-Type", "application/json; charset=UTF-8");
            connection.setRequestProperty("Accept", "application/json");
            if (bearerToken != null && !bearerToken.isEmpty()) { connection.setRequestProperty("Authorization", "Bearer " + bearerToken); }

            byte[] payload = MiniJson.stringify(body).getBytes(StandardCharsets.UTF_8);
            OutputStream outputStream = connection.getOutputStream();
            outputStream.write(payload);
            outputStream.flush();
            outputStream.close();

            int statusCode = connection.getResponseCode();
            String responseText = readAll(statusCode >= 400 ? connection.getErrorStream() : connection.getInputStream());
            Object parsed = responseText.isEmpty() ? new LinkedHashMap<String, Object>() : MiniJson.parse(responseText);
            if (!(parsed instanceof Map)) { throw new IOException("Unexpected response: " + responseText); }
            Map<String, Object> result = (Map<String, Object>) parsed;
            if (statusCode >= 400) { throw new IOException("HTTP " + statusCode + ": " + asString(result.get("error"))); }
            return result;
        } finally {
            if (connection != null) { connection.disconnect(); }
        }
    }

    private EncodedPayload encodePayload(Map<String, Object> payload) throws IOException {
        Map<String, Object> safePayload = payload == null ? new LinkedHashMap<String, Object>() : payload;
        String payloadJson = MiniJson.stringify(safePayload);
        byte[] raw = payloadJson.getBytes(StandardCharsets.UTF_8);
        if (raw.length < COMPRESS_PAYLOAD_THRESHOLD_BYTES) {
            return EncodedPayload.identity(safePayload);
        }
        byte[] compressed = gzip(raw);
        int wireSize = ((compressed.length + 2) / 3) * 4;
        if (wireSize >= raw.length) {
            return EncodedPayload.identity(safePayload);
        }
        return EncodedPayload.compressed(Base64Util.encode(compressed), PAYLOAD_ENCODING_GZIP_BASE64_JSON, raw.length, compressed.length);
    }

    private byte[] gzip(byte[] input) throws IOException {
        ByteArrayOutputStream outputStream = new ByteArrayOutputStream();
        GZIPOutputStream gzipOutputStream = new GZIPOutputStream(outputStream);
        gzipOutputStream.write(input);
        gzipOutputStream.finish();
        gzipOutputStream.close();
        return outputStream.toByteArray();
    }

    private static final class EncodedPayload {
        private final Object payload;
        private final String encoding;
        private final int rawSize;
        private final int compressedSize;

        private EncodedPayload(Object payload, String encoding, int rawSize, int compressedSize) {
            this.payload = payload;
            this.encoding = encoding;
            this.rawSize = rawSize;
            this.compressedSize = compressedSize;
        }

        private static EncodedPayload identity(Object payload) {
            return new EncodedPayload(payload, "", 0, 0);
        }

        private static EncodedPayload compressed(String payload, String encoding, int rawSize, int compressedSize) {
            return new EncodedPayload(payload, encoding, rawSize, compressedSize);
        }
    }
    private void notifyError(Throwable throwable) {
        ErrorHandler handler = errorHandler;
        if (handler != null) {
            handler.onError(throwable);
            return;
        }
        if (throwable == null) {
            return;
        }
        Log.e(TAG, "relay client error", throwable);
    }

    private void ensureLoggedIn() throws IOException {
        if (token == null || token.isEmpty()) {
            login();
        }
    }

    private void clearSession() {
        token = null;
        wsUrl = null;
    }

    private boolean isUnauthorized(IOException ex) {
        String message = safeMessage(ex);
        return message.contains("401") || message.contains("unauthorized") || message.contains("Unauthorized");
    }

    private long computeRetryDelayMs(int attempt) {
        int safeAttempt = Math.max(0, Math.min(attempt, 6));
        long capped = BASE_RETRY_DELAY_MS * (1L << safeAttempt);
        if (capped < BASE_RETRY_DELAY_MS) {
            capped = BASE_RETRY_DELAY_MS;
        }
        capped = Math.min(capped, MAX_RETRY_DELAY_MS);
        if (capped <= BASE_RETRY_DELAY_MS) {
            return BASE_RETRY_DELAY_MS;
        }
        return BASE_RETRY_DELAY_MS + randomLong(capped - BASE_RETRY_DELAY_MS + 1L);
    }

    private long computeHeartbeatDelayMs() {
        return HEARTBEAT_INTERVAL_MS + randomLong(HEARTBEAT_JITTER_MS + 1L);
    }

    private boolean requiresSerializedRelayHandler(RelayHandler handler) {
        return hasAutoBindFields(handler == null ? null : handler.getClass());
    }

    private RelayHandler createRequestScopedRelayHandler(RelayHandler handler) {
        if (handler == null) {
            return null;
        }
        Class<?> handlerClass = handler.getClass();
        if (!hasAutoBindFields(handlerClass)) {
            return handler;
        }
        try {
            java.lang.reflect.Constructor<?> constructor = handlerClass.getDeclaredConstructor();
            constructor.setAccessible(true);
            Object instance = constructor.newInstance();
            if (instance instanceof RelayHandler) {
                return (RelayHandler) instance;
            }
        } catch (Throwable ignore) {
        }
        return handler;
    }

    private boolean hasAutoBindFields(Class<?> type) {
        Class<?> current = type;
        while (current != null && current != Object.class) {
            Field[] fields = current.getDeclaredFields();
            for (Field field : fields) {
                if (field.getAnnotation(AutoBind.class) != null) {
                    return true;
                }
            }
            current = current.getSuperclass();
        }
        return false;
    }

    private void ensureJobExecutor() {
        int desired = maxInFlight <= 0 ? 1 : maxInFlight;
        synchronized (executorLock) {
            if (jobExecutor != null && jobExecutor.getCorePoolSize() == desired) {
                return;
            }
            ThreadPoolExecutor previous = jobExecutor;
            jobExecutor = new ThreadPoolExecutor(
                desired,
                desired,
                60L,
                TimeUnit.SECONDS,
                new LinkedBlockingQueue<Runnable>(Math.max(desired * 2, 64)),
                new ThreadFactory() {
                    @Override
                    public Thread newThread(Runnable runnable) {
                        Thread thread = new Thread(runnable, "r0rpc-job-worker");
                        thread.setDaemon(true);
                        return thread;
                    }
                }
            );
            if (previous != null) {
                previous.shutdownNow();
            }
        }
    }

    private void shutdownJobExecutor() {
        synchronized (executorLock) {
            if (jobExecutor == null) {
                return;
            }
            jobExecutor.shutdownNow();
            jobExecutor = null;
        }
    }

    private boolean isServerSilent() {
        long lastSeenAt = lastServerActivityAt;
        return lastSeenAt > 0L && System.currentTimeMillis() - lastSeenAt > computeServerSilenceTimeoutMs();
    }

    private long computeServerSilenceTimeoutMs() {
        long timeoutMs = SERVER_SILENCE_TIMEOUT_MS;
        if (readTimeoutMs > 0) {
            timeoutMs = Math.min(timeoutMs, Math.max(10000L, (long) readTimeoutMs));
        }
        return Math.max(10000L, timeoutMs);
    }

    private long randomLong(long boundExclusive) {
        if (boundExclusive <= 0L) {
            return 0L;
        }
        return ThreadLocalRandom.current().nextLong(boundExclusive);
    }

    private String safeMessage(Throwable throwable) {
        if (throwable == null || throwable.getMessage() == null) {
            return throwable == null ? "unknown" : throwable.getClass().getSimpleName();
        }
        return throwable.getMessage();
    }

    private String currentWsUrl() { return wsUrl == null || wsUrl.isEmpty() ? buildWsUrl() : wsUrl; }

    private String buildWsUrl() {
        String wsBase = baseUrl;
        if (wsBase.startsWith("https://")) {
            wsBase = "wss://" + wsBase.substring("https://".length());
        } else if (wsBase.startsWith("http://")) {
            wsBase = "ws://" + wsBase.substring("http://".length());
        }
        return wsBase + "/api/client/ws?token=" + urlEncode(token == null ? "" : token);
    }

    private static String normalizeBaseUrl(String value) {
        String normalized = value == null ? "" : value.trim();
        if (normalized.isEmpty()) {
            throw new IllegalArgumentException("baseUrl can not be empty");
        }
        if (!normalized.startsWith("http://") && !normalized.startsWith("https://")) {
            normalized = "http://" + normalized;
        }
        return trimTrailingSlash(normalized);
    }

    private static String trimTrailingSlash(String value) {
        if (value.endsWith("/")) { return value.substring(0, value.length() - 1); }
        return value;
    }

    private static String readAll(InputStream inputStream) throws IOException {
        if (inputStream == null) { return ""; }
        BufferedReader reader = new BufferedReader(new InputStreamReader(inputStream, StandardCharsets.UTF_8));
        StringBuilder builder = new StringBuilder();
        String line;
        while ((line = reader.readLine()) != null) { builder.append(line); }
        return builder.toString();
    }

    private static String asString(Object value) { return value == null ? "" : String.valueOf(value); }

    private static String urlEncode(String value) {
        StringBuilder builder = new StringBuilder();
        byte[] bytes = value.getBytes(StandardCharsets.UTF_8);
        for (int i = 0; i < bytes.length; i++) {
            int item = bytes[i] & 0xff;
            boolean safe = (item >= 'a' && item <= 'z') || (item >= 'A' && item <= 'Z') || (item >= '0' && item <= '9') || item == '-' || item == '_' || item == '.' || item == '~';
            if (safe) {
                builder.append((char) item);
            } else {
                builder.append('%');
                String hex = Integer.toHexString(item).toUpperCase();
                if (hex.length() == 1) { builder.append('0'); }
                builder.append(hex);
            }
        }
        return builder.toString();
    }

    private static void sleepQuietly(long millis) {
        try { Thread.sleep(millis); } catch (InterruptedException ignore) { Thread.currentThread().interrupt(); }
    }

    private static void closeQuietly(SimpleWebSocket socket) {
        if (socket == null) { return; }
        try { socket.close(); } catch (Exception ignore) {}
    }
}






