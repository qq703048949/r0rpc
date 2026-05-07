package com.r0rpc.xposed.fish.handler;

import android.util.Log;

import com.r0rpc.relay.api.RelayHandler;
import com.r0rpc.relay.api.RelayRequest;
import com.r0rpc.relay.api.RelayResponse;
import com.r0rpc.relay.api.databind.AutoBind;
import com.r0rpc.xposed.utils.Store;

import de.robv.android.xposed.XposedHelpers;

public class DecryptHandler implements RelayHandler {

    public static final String TAG = "Fish";

    @AutoBind
    private String encode_str;

    @Override
    public void handleRequest(RelayRequest relayRequest, RelayResponse relayResponse) throws Exception {
        Log.e(TAG, "payload is:" + relayRequest.getPayload());
        Log.e(TAG, "encode_str is:" + encode_str);
        ClassLoader classLoader = Store.appClassLoader.get("classloader");
        Class DecryptUtils = XposedHelpers.findClass("com.taobao.android.remoteobject.easy.network.interceptor.DecryptUtils", classLoader);
        String ret = (String) XposedHelpers.callStaticMethod(DecryptUtils, "doDecode", encode_str);
        relayResponse.success(ret);
    }
}
