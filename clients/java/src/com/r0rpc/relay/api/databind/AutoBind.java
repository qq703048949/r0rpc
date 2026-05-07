package com.r0rpc.relay.api.databind;

import java.lang.annotation.ElementType;
import java.lang.annotation.Retention;
import java.lang.annotation.RetentionPolicy;
import java.lang.annotation.Target;

@Retention(RetentionPolicy.RUNTIME)
@Target(ElementType.FIELD)
public @interface AutoBind {
    String key() default "";
    String defaultStringValue() default "";
    int defaultIntValue() default 0;
    long defaultLongValue() default 0L;
    boolean defaultBooleanValue() default false;
}
