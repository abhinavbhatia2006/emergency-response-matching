package com.example.emergency.coordinator;
import com.example.emergency.common.Contracts.Node;
import jakarta.validation.Valid;
import org.springframework.web.bind.annotation.*;
import org.springframework.scheduling.annotation.Scheduled;
import java.time.Instant;
import java.util.*;
import java.util.concurrent.ConcurrentHashMap;
@RestController
@RequestMapping("/internal")
public class TopologyController {
    private final ShardStore store;
    private final Map<String,Map<String,Object>> nodes=new ConcurrentHashMap<>();
    public TopologyController(ShardStore store){this.store=store;}
    @PostMapping({"/nodes/register","/nodes/heartbeat"}) public Object heartbeat(@Valid @RequestBody Node node){
        nodes.put(node.nodeId(),Map.of("nodeId",node.nodeId(),"shard",node.shard(),"role",node.role(),"healthy",node.healthy(),"lagBytes",node.lagBytes(),"lastSeen",Instant.now().toString()));
        return Map.of("accepted",true);
    }
    @Scheduled(fixedDelay=5000) public void probe(){
        for(int i=0;i<3;i++) {
            try {Boolean standby=store.shard(i).queryForObject("SELECT pg_is_in_recovery()",Boolean.class);heartbeat(new Node("shard-"+i+"-write-endpoint",i,Boolean.TRUE.equals(standby)?"STANDBY":"PRIMARY",true,0));}
            catch(Exception e){heartbeat(new Node("shard-"+i+"-write-endpoint",i,"UNKNOWN",false,-1));}
        }
    }
    @GetMapping("/topology") public Object topology(){
        var result=new ArrayList<Map<String,Object>>();
        for(var node:nodes.values()) {var row=new LinkedHashMap<>(node);row.put("stale",Instant.parse(node.get("lastSeen").toString()).isBefore(Instant.now().minusSeconds(20)));result.add(row);}
        result.sort(Comparator.comparing(r->r.get("nodeId").toString()));
        var counts=new ArrayList<Map<String,Object>>();
        for(int i=0;i<3;i++)try {counts.add(Map.of("shard",i,"requests",store.shard(i).queryForObject("SELECT count(*) FROM emergency_requests",Long.class),"resources",store.shard(i).queryForObject("SELECT count(*) FROM resources",Long.class),"outboxPending",store.shard(i).queryForObject("SELECT count(*) FROM outbox_events WHERE state='NEW'",Long.class)));}catch(Exception e){counts.add(Map.of("shard",i,"unavailable",true));}
        return Map.of("nodes",result,"shards",counts,"observedAt",Instant.now().toString());
    }
}
