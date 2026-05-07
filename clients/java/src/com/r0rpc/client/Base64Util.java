package com.r0rpc.client;

public final class Base64Util {
    private static final char[] TABLE = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/".toCharArray();

    private Base64Util() {
    }

    public static String encode(byte[] input) {
        if (input == null || input.length == 0) {
            return "";
        }
        StringBuilder builder = new StringBuilder(((input.length + 2) / 3) * 4);
        int index = 0;
        while (index < input.length) {
            int b0 = input[index++] & 0xff;
            int b1 = index < input.length ? input[index++] & 0xff : -1;
            int b2 = index < input.length ? input[index++] & 0xff : -1;

            builder.append(TABLE[b0 >>> 2]);
            builder.append(TABLE[((b0 & 0x03) << 4) | (b1 >= 0 ? (b1 >>> 4) : 0)]);
            builder.append(b1 >= 0 ? TABLE[((b1 & 0x0f) << 2) | (b2 >= 0 ? (b2 >>> 6) : 0)] : '=');
            builder.append(b2 >= 0 ? TABLE[b2 & 0x3f] : '=');
        }
        return builder.toString();
    }
}
