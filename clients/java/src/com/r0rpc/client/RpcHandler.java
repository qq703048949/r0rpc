package com.r0rpc.client;

import java.util.Map;

public interface RpcHandler {
    RpcResponse handle(Map<String, Object> payload) throws Exception;
}
