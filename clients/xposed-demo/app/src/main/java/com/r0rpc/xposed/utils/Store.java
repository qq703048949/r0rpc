package com.r0rpc.xposed.utils;

import android.app.Activity;
import android.content.Context;


import java.util.Map;
import java.util.concurrent.ConcurrentHashMap;

public class Store {
    public static Map<String, ClassLoader> appClassLoader = new ConcurrentHashMap<>();
    public static Map<String, Context> appContext = new ConcurrentHashMap<>();
    public static Map<String, Object> appObject = new ConcurrentHashMap<>();
}
