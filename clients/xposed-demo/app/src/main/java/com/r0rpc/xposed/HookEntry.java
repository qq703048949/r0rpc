package com.r0rpc.xposed;
import android.util.Log;
import com.r0rpc.xposed.fish.Fish;
import de.robv.android.xposed.IXposedHookLoadPackage;
import de.robv.android.xposed.callbacks.XC_LoadPackage;
public class HookEntry implements IXposedHookLoadPackage {
    public static final String tag = "HookEntry";

    public void handleLoadPackage(final XC_LoadPackage.LoadPackageParam loadPackageParam) throws Throwable {


        Log.e(tag, "enter HookEntry!" + loadPackageParam.packageName);
         if (loadPackageParam.packageName.equals("com.taobao.idlefish")) {
            Fish.entry(loadPackageParam);
        }
    }
}
