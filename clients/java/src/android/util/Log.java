package android.util;

public final class Log {
    private Log() {
    }

    public static int i(String tag, String msg) {
        System.out.println(format("I", tag, msg));
        return 0;
    }

    public static int w(String tag, String msg) {
        System.out.println(format("W", tag, msg));
        return 0;
    }

    public static int e(String tag, String msg) {
        System.err.println(format("E", tag, msg));
        return 0;
    }

    public static int e(String tag, String msg, Throwable throwable) {
        System.err.println(format("E", tag, msg));
        if (throwable != null) {
            throwable.printStackTrace(System.err);
        }
        return 0;
    }

    private static String format(String level, String tag, String msg) {
        return "[" + level + "/" + tag + "] " + (msg == null ? "" : msg);
    }
}