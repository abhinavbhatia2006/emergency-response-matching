package com.example.emergency.matching;
import java.util.*;
public final class MatchingPolicy {
    private MatchingPolicy() {}
    public static double distance(double a,double b,double c,double d) {
        double x=Math.toRadians(c-a),y=Math.toRadians(d-b);
        double h=Math.pow(Math.sin(x/2),2)+Math.cos(Math.toRadians(a))*Math.cos(Math.toRadians(c))*Math.pow(Math.sin(y/2),2);
        return 6371.0088*2*Math.asin(Math.sqrt(Math.clamp(h,0,1)));
    }
    @SuppressWarnings("unchecked") public static double distance(Map<String,Object> a,Map<String,Object> b) {
        var x=(Map<String,Object>)a.get("location");var y=(Map<String,Object>)b.get("location");
        return distance(((Number)x.get("latitude")).doubleValue(),((Number)x.get("longitude")).doubleValue(),((Number)y.get("latitude")).doubleValue(),((Number)y.get("longitude")).doubleValue());
    }
    public static List<Map<String,Object>> rank(Map<String,Object> request,List<Map<String,Object>> resources,double radius) {
        return resources.stream().filter(r->request.get("type").equals(r.get("type"))&&Boolean.TRUE.equals(r.get("availability"))&&"AVAILABLE".equals(r.get("status")))
            .filter(r->distance(request,r)<=radius).sorted(Comparator.<Map<String,Object>>comparingDouble(r->distance(request,r)).thenComparing(r->r.get("id").toString())).toList();
    }
}
