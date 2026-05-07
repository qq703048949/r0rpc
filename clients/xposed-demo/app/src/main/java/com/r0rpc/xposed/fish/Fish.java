package com.r0rpc.xposed.fish;

import android.app.Application;
import android.content.Context;
import android.os.Build;
import android.provider.Settings;
import android.util.Log;
import android.widget.Toast;

import com.r0rpc.client.RelayClient;
import com.r0rpc.xposed.fish.handler.DecryptHandler;
import com.r0rpc.xposed.fish.handler.PingHandler;
import com.r0rpc.xposed.utils.Store;

import java.util.concurrent.atomic.AtomicBoolean;

import de.robv.android.xposed.XC_MethodHook;
import de.robv.android.xposed.XposedBridge;
import de.robv.android.xposed.XposedHelpers;
import de.robv.android.xposed.callbacks.XC_LoadPackage;


public class Fish {
    public static String TAG = "Fish";
    private static final AtomicBoolean STARTED = new AtomicBoolean(false);

    public static void entry(XC_LoadPackage.LoadPackageParam loadPackageParam) throws ClassNotFoundException {
        if (!loadPackageParam.processName.equals(loadPackageParam.packageName)) {
            return;
        }
        Log.e(TAG, loadPackageParam.processName + "=======" + loadPackageParam.packageName);

        Class ActivityThread = XposedHelpers.findClass("android.app.ActivityThread", loadPackageParam.classLoader);
        XposedBridge.hookAllMethods(ActivityThread, "performLaunchActivity", new XC_MethodHook() {
            @Override
            protected void afterHookedMethod(MethodHookParam param) throws Throwable {
                super.afterHookedMethod(param);
                Application mInitialApplication = (Application) XposedHelpers.getObjectField(param.thisObject, "mInitialApplication");
                ClassLoader finalCL = (ClassLoader) XposedHelpers.callMethod(mInitialApplication, "getClassLoader");
                Store.appClassLoader.put("classloader", finalCL);
                Store.appContext.put("context", mInitialApplication);
                startRpcOnce(mInitialApplication);

            }
        });
    }

    private static void startRpcOnce(final Context context) {
        if (!STARTED.compareAndSet(false, true)) {
            return;
        }
        String androidId = Settings.Secure.getString(context.getContentResolver(), Settings.Secure.ANDROID_ID);
        Log.e(TAG, "context:" + context + " androidId:" + androidId);
        Toast.makeText(context, "androidId:" + androidId, Toast.LENGTH_LONG).show();
        RelayClient relayClient = new RelayClient("159.75.100.225:9876", "admin", "123456", androidId, "idlefish", Build.MANUFACTURER).maxInFlight(1024);
        relayClient.registerHandler("ping", new PingHandler());
        relayClient.registerHandler("decrypt", new DecryptHandler());
        relayClient.start();
        Log.e(TAG, "start ok");
    }
}
