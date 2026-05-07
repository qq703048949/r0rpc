package com.r0rpc.xposed.fish.handler;

import com.r0rpc.relay.api.RelayHandler;
import com.r0rpc.relay.api.RelayRequest;
import com.r0rpc.relay.api.RelayResponse;

import java.util.LinkedHashMap;
import java.util.Map;

public class PingHandler implements RelayHandler {
    @Override
    public void handleRequest(RelayRequest request, RelayResponse response) {
        Map<String, Object> result = new LinkedHashMap<String, Object>();
        response.success(result);
    }
}
