package com.example.emergency.coordinator;

import java.util.UUID;

public final class ShardRouter {
    private ShardRouter() {}
    public static int shardFor(UUID id) {
        return Math.floorMod(id.getMostSignificantBits() ^ id.getLeastSignificantBits(), 3);
    }
}
