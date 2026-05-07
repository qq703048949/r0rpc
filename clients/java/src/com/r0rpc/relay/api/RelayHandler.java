package com.r0rpc.relay.api;

public interface RelayHandler {
    void handleRequest(RelayRequest request, RelayResponse response) throws Exception;
}
