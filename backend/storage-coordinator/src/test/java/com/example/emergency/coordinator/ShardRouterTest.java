package com.example.emergency.coordinator;
import org.junit.jupiter.api.Test;
import java.util.UUID;
import static org.junit.jupiter.api.Assertions.*;
class ShardRouterTest {
    @Test void fixedVectorsHandleSignedLongExtremes() {
        assertEquals(0,ShardRouter.shardFor(new UUID(0,0)));
        assertEquals(1,ShardRouter.shardFor(new UUID(0,1)));
        assertEquals(2,ShardRouter.shardFor(new UUID(0,2)));
        assertEquals(2,ShardRouter.shardFor(new UUID(0,-1)));
        assertEquals(1,ShardRouter.shardFor(new UUID(Long.MIN_VALUE,0)));
    }
    @Test void deterministicSequenceUsesEveryShard() {
        int[] counts = new int[3];
        for(int i=0;i<300;i++) counts[ShardRouter.shardFor(new UUID(0,i))]++;
        assertArrayEquals(new int[]{100,100,100},counts);
    }
}
