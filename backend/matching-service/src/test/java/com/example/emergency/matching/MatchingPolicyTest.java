package com.example.emergency.matching;
import java.util.*;
import org.junit.jupiter.api.Test;
import static org.junit.jupiter.api.Assertions.*;
class MatchingPolicyTest {
    @Test void fiftyKilometreBoundaryIsInclusiveAndAvailabilityIsRequired() {
        double edge=Math.toDegrees(50/6371.0088);
        var request=resource("request","WATER",0,true);
        var at=resource("edge","WATER",edge,true);
        var outside=resource("outside","WATER",edge+0.000001,true);
        assertEquals(List.of("edge"),MatchingPolicy.rank(request,List.of(outside,at),50).stream().map(r->r.get("id")).toList());
        assertTrue(MatchingPolicy.rank(request,List.of(resource("unavailable","WATER",0,false)),50).isEmpty());
    }
    Map<String,Object> resource(String id,String type,double lat,boolean available) {return Map.of("id",id,"type",type,"availability",available,"status",available?"AVAILABLE":"UNAVAILABLE","location",Map.of("latitude",lat,"longitude",0));}
    @Test void distancesHandleDatelineAndPoles() {
        assertEquals(0,MatchingPolicy.distance(0,0,0,0),1e-9);
        assertEquals(111.195,MatchingPolicy.distance(0,0,1,0),0.01);
        assertEquals(222.39,MatchingPolicy.distance(0,179,0,-179),0.1);
        assertTrue(Double.isFinite(MatchingPolicy.distance(90,0,-90,0)));
    }
    @Test void filtersAndRanksWithStableTieBreak() {
        var request=resource("request","WATER",0,true);
        var candidates=List.of(resource("z","WATER",0.1,true),resource("a","WATER",0.1,true),resource("wrong","FOOD",0,true),resource("far","WATER",20,true),resource("busy","WATER",0,false));
        assertEquals(List.of("a","z"),MatchingPolicy.rank(request,candidates,50).stream().map(r->r.get("id")).toList());
    }
}
