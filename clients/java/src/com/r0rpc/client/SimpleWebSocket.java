package com.r0rpc.client;

import java.io.BufferedInputStream;
import java.io.BufferedOutputStream;
import java.io.ByteArrayOutputStream;
import java.io.Closeable;
import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.net.InetSocketAddress;
import java.net.Socket;
import java.net.URI;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.security.SecureRandom;
import java.util.Map;

import javax.net.ssl.SSLSocketFactory;

public final class SimpleWebSocket implements Closeable {
    private static final String WS_GUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11";
    private static final int OPCODE_TEXT = 0x1;
    private static final int OPCODE_CLOSE = 0x8;
    private static final int OPCODE_PING = 0x9;
    private static final int OPCODE_PONG = 0xA;

    private final Socket socket;
    private final InputStream inputStream;
    private final OutputStream outputStream;
    private final Object writeLock = new Object();
    private volatile boolean closed;

    private SimpleWebSocket(Socket socket) throws IOException {
        this.socket = socket;
        this.inputStream = new BufferedInputStream(socket.getInputStream());
        this.outputStream = new BufferedOutputStream(socket.getOutputStream());
    }

    public static SimpleWebSocket connect(String wsUrl, Map<String, String> headers, int connectTimeoutMs, int readTimeoutMs) throws IOException {
        try {
            URI uri = URI.create(wsUrl);
            String scheme = uri.getScheme();
            boolean secure = "wss".equalsIgnoreCase(scheme);
            if (!secure && !"ws".equalsIgnoreCase(scheme)) {
                throw new IOException("Unsupported websocket scheme: " + scheme);
            }

            int port = uri.getPort();
            if (port <= 0) {
                port = secure ? 443 : 80;
            }

            Socket socket = secure ? SSLSocketFactory.getDefault().createSocket() : new Socket();
            socket.connect(new InetSocketAddress(uri.getHost(), port), connectTimeoutMs);
            socket.setSoTimeout(readTimeoutMs);

            SimpleWebSocket webSocket = new SimpleWebSocket(socket);
            webSocket.handshake(uri, headers);
            return webSocket;
        } catch (IOException ex) {
            throw ex;
        } catch (Exception ex) {
            throw new IOException("WebSocket connect failed: " + ex, ex);
        }
    }

    private void handshake(URI uri, Map<String, String> headers) throws Exception {
        byte[] nonce = new byte[16];
        new SecureRandom().nextBytes(nonce);
        String key = Base64Util.encode(nonce);

        String path = uri.getRawPath();
        if (path == null || path.isEmpty()) {
            path = "/";
        }
        if (uri.getRawQuery() != null && !uri.getRawQuery().isEmpty()) {
            path += "?" + uri.getRawQuery();
        }

        String host = uri.getHost();
        int port = uri.getPort();
        if (port > 0) {
            host += ":" + port;
        }

        StringBuilder request = new StringBuilder();
        request.append("GET ").append(path).append(" HTTP/1.1\r\n");
        request.append("Host: ").append(host).append("\r\n");
        request.append("Upgrade: websocket\r\n");
        request.append("Connection: Upgrade\r\n");
        request.append("Sec-WebSocket-Version: 13\r\n");
        request.append("Sec-WebSocket-Key: ").append(key).append("\r\n");
        request.append("User-Agent: R0RPC-Java-Client\r\n");
        if (headers != null) {
            for (Map.Entry<String, String> entry : headers.entrySet()) {
                request.append(entry.getKey()).append(": ").append(entry.getValue()).append("\r\n");
            }
        }
        request.append("\r\n");

        outputStream.write(request.toString().getBytes(StandardCharsets.UTF_8));
        outputStream.flush();

        String statusLine = readHttpLine(inputStream);
        if (statusLine == null || !statusLine.contains(" 101 ")) {
            StringBuilder response = new StringBuilder();
            String line;
            while ((line = readHttpLine(inputStream)) != null && !line.isEmpty()) {
                response.append(line).append('\n');
            }
            throw new IOException("WebSocket handshake failed: " + statusLine + " " + response.toString().trim());
        }

        String acceptHeader = null;
        String line;
        while ((line = readHttpLine(inputStream)) != null && !line.isEmpty()) {
            int separator = line.indexOf(':');
            if (separator <= 0) {
                continue;
            }
            String name = line.substring(0, separator).trim();
            String value = line.substring(separator + 1).trim();
            if ("Sec-WebSocket-Accept".equalsIgnoreCase(name)) {
                acceptHeader = value;
            }
        }

        MessageDigest digest = MessageDigest.getInstance("SHA-1");
        String expectedAccept = Base64Util.encode(digest.digest((key + WS_GUID).getBytes(StandardCharsets.UTF_8)));
        if (!expectedAccept.equals(acceptHeader)) {
            throw new IOException("WebSocket handshake verification failed");
        }
    }

    public boolean isOpen() {
        return !closed && !socket.isClosed();
    }

    public String readText() throws IOException {
        while (isOpen()) {
            Frame frame;
            try {
                frame = readFrame();
            } catch (IOException ex) {
                abortSocket();
                throw ex;
            }
            if (frame == null) {
                return null;
            }
            switch (frame.opcode) {
                case OPCODE_TEXT:
                    return new String(frame.payload, StandardCharsets.UTF_8);
                case OPCODE_PING:
                    sendControl(OPCODE_PONG, frame.payload);
                    break;
                case OPCODE_PONG:
                    break;
                case OPCODE_CLOSE:
                    close();
                    return null;
                default:
                    break;
            }
        }
        return null;
    }

    public void sendText(String text) throws IOException {
        sendFrame(OPCODE_TEXT, text == null ? new byte[0] : text.getBytes(StandardCharsets.UTF_8));
    }

    public void sendHeartbeat() throws IOException {
        sendText("{\"type\":\"heartbeat\"}");
    }

    private void sendControl(int opcode, byte[] payload) throws IOException {
        sendFrame(opcode, payload == null ? new byte[0] : payload);
    }

    private void sendFrame(int opcode, byte[] payload) throws IOException {
        if (!isOpen()) {
            throw new IOException("WebSocket is closed");
        }
        if (payload == null) {
            payload = new byte[0];
        }

        byte[] mask = new byte[4];
        new SecureRandom().nextBytes(mask);

        synchronized (writeLock) {
            try {
                outputStream.write(0x80 | (opcode & 0x0f));
                if (payload.length < 126) {
                    outputStream.write(0x80 | payload.length);
                } else if (payload.length <= 0xffff) {
                    outputStream.write(0x80 | 126);
                    outputStream.write((payload.length >>> 8) & 0xff);
                    outputStream.write(payload.length & 0xff);
                } else {
                    outputStream.write(0x80 | 127);
                    long length = payload.length;
                    for (int shift = 56; shift >= 0; shift -= 8) {
                        outputStream.write((int) ((length >>> shift) & 0xff));
                    }
                }
                outputStream.write(mask);
                for (int i = 0; i < payload.length; i++) {
                    outputStream.write(payload[i] ^ mask[i % 4]);
                }
                outputStream.flush();
            } catch (IOException ex) {
                abortSocket();
                throw ex;
            }
        }
    }

    private Frame readFrame() throws IOException {
        int first = inputStream.read();
        if (first < 0) {
            return null;
        }
        int second = readByte();
        boolean fin = (first & 0x80) != 0;
        if (!fin) {
            throw new IOException("Fragmented websocket frames are not supported");
        }
        int opcode = first & 0x0f;
        int payloadLength = second & 0x7f;
        if (payloadLength == 126) {
            payloadLength = (readByte() << 8) | readByte();
        } else if (payloadLength == 127) {
            long length = 0L;
            for (int i = 0; i < 8; i++) {
                length = (length << 8) | readByte();
            }
            if (length > Integer.MAX_VALUE) {
                throw new IOException("WebSocket payload too large");
            }
            payloadLength = (int) length;
        }

        boolean masked = (second & 0x80) != 0;
        byte[] mask = null;
        if (masked) {
            mask = readFully(4);
        }
        byte[] payload = readFully(payloadLength);
        if (masked && mask != null) {
            for (int i = 0; i < payload.length; i++) {
                payload[i] ^= mask[i % 4];
            }
        }
        return new Frame(opcode, payload);
    }

    private int readByte() throws IOException {
        int value = inputStream.read();
        if (value < 0) {
            throw new IOException("Unexpected end of stream");
        }
        return value;
    }

    private byte[] readFully(int length) throws IOException {
        byte[] data = new byte[length];
        int offset = 0;
        while (offset < length) {
            int read = inputStream.read(data, offset, length - offset);
            if (read < 0) {
                throw new IOException("Unexpected end of stream");
            }
            offset += read;
        }
        return data;
    }

    private static String readHttpLine(InputStream inputStream) throws IOException {
        ByteArrayOutputStream buffer = new ByteArrayOutputStream();
        int previous = -1;
        while (true) {
            int current = inputStream.read();
            if (current < 0) {
                if (buffer.size() == 0) {
                    return null;
                }
                break;
            }
            if (previous == '\r' && current == '\n') {
                break;
            }
            if (previous >= 0) {
                buffer.write(previous);
            }
            previous = current;
        }
        return new String(buffer.toByteArray(), StandardCharsets.UTF_8);
    }

    @Override
    public void close() throws IOException {
        if (closed) {
            return;
        }
        try {
            sendControl(OPCODE_CLOSE, new byte[0]);
        } catch (Exception ignore) {
        }
        abortSocket();
    }

    private void abortSocket() {
        if (closed) {
            return;
        }
        closed = true;
        try {
            socket.close();
        } catch (IOException ignore) {
        }
    }

    private static final class Frame {
        private final int opcode;
        private final byte[] payload;

        private Frame(int opcode, byte[] payload) {
            this.opcode = opcode;
            this.payload = payload == null ? new byte[0] : payload;
        }
    }
}
