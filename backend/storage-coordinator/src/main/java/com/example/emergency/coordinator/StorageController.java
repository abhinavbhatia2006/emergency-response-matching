package com.example.emergency.coordinator;

import com.example.emergency.common.*;
import com.example.emergency.common.Contracts.*;
import jakarta.validation.Valid;
import org.springframework.jdbc.core.JdbcTemplate;
import org.springframework.web.bind.annotation.*;
import java.util.*;

@RestController
@RequestMapping("/internal")
public class StorageController {
    private final ShardStore store;
    public StorageController(ShardStore store) { this.store = store; }
    static Map<String,Object> one(JdbcTemplate db,String sql,Object... args) {
        var rows=db.queryForList(sql,args);
        if(rows.isEmpty()) throw ApiException.missing();
        return rows.getFirst();
    }
    static Map<String,Object> view(Map<String,Object> row) {
        var result=new LinkedHashMap<String,Object>();
        row.forEach((key,value)->{
            StringBuilder name=new StringBuilder(); boolean upper=false;
            for(char c:key.toCharArray()) { if(c=='_') upper=true; else { name.append(upper?Character.toUpperCase(c):c);upper=false; } }
            if(value instanceof java.sql.Timestamp t)value=t.toInstant().toString();
            result.put(name.toString(),value);
        });
        if(row.containsKey("latitude")) {
            result.put("location",Map.of("label",row.get("location_text"),"latitude",row.get("latitude"),"longitude",row.get("longitude")));
            result.remove("locationText");result.remove("latitude");result.remove("longitude");
        }
        if(result.containsKey("available"))result.put("availability",result.remove("available"));
        return result;
    }
    private Map<String,Object> request(JdbcTemplate db,UUID id,boolean lock) {
        return one(db,"SELECT * FROM emergency_requests WHERE id=?"+(lock?" FOR UPDATE":""),id);
    }
    private void event(JdbcTemplate db,UUID id) { db.update("INSERT INTO outbox_events(id,request_id) VALUES (?,?)",UUID.randomUUID(),id); }
    @PostMapping("/requests") public Object create(@Valid @RequestBody NewRequest body) {
        UUID id=UUID.randomUUID();
        return store.transaction(id,db->{
            db.update("INSERT INTO emergency_requests(id,type,location_text,latitude,longitude,details,priority) VALUES (?,?,?,?,?,?,?)",id,body.type().name(),body.location().label(),body.location().latitude(),body.location().longitude(),body.details(),body.priority()==null?"NORMAL":body.priority());
            event(db,id);return view(request(db,id,false));
        });
    }
    @PostMapping("/resources") public Object createResource(@Valid @RequestBody NewResource body) {
        UUID id=UUID.randomUUID();var db=store.owner(id);
        db.update("INSERT INTO resources(id,type,location_text,latitude,longitude,description,available,status) VALUES (?,?,?,?,?,?,?,?)",id,body.type().name(),body.location().label(),body.location().latitude(),body.location().longitude(),body.description()==null?"":body.description(),body.availability(),body.availability()?"AVAILABLE":"UNAVAILABLE");
        return view(one(db,"SELECT * FROM resources WHERE id=?",id));
    }
    @GetMapping("/requests/{id}") public Object get(@PathVariable UUID id) { return store.read(id,db->publicView(request(db,id,false))); }
    private Map<String,Object> publicView(Map<String,Object> row) {
        var result=view(row);result.remove("leaseToken");result.remove("leaseUntil");result.remove("candidateResourceId");return result;
    }
    @GetMapping("/resources/{id}") public Object getResource(@PathVariable UUID id) { return store.read(id,db->view(one(db,"SELECT * FROM resources WHERE id=?",id))); }
    @GetMapping("/requests") public Object requests(@RequestParam(defaultValue="0") int offset,@RequestParam(defaultValue="50") int limit) { return list("emergency_requests",null,offset,limit); }
    @GetMapping("/resources") public Object resources(@RequestParam(required=false) Type type,@RequestParam(defaultValue="0") int offset,@RequestParam(defaultValue="50") int limit) { return list("resources",type,offset,limit); }
    private List<Map<String,Object>> list(String table,Type type,int offset,int limit) {
        if(offset<0||offset>10000||limit<1||limit>200)throw new IllegalArgumentException();
        var rows=new ArrayList<Map<String,Object>>();
        for(int i=0;i<3;i++) {
            String sql="SELECT * FROM "+table+(type==null?"":" WHERE type=?")+" ORDER BY created_at DESC,id LIMIT ?";
            rows.addAll(type==null?store.shard(i).queryForList(sql,offset+limit):store.shard(i).queryForList(sql,type.name(),offset+limit));
        }
        rows.sort(Comparator.<Map<String,Object>,String>comparing(r->r.get("created_at").toString()).reversed().thenComparing(r->r.get("id").toString()));
        return rows.stream().skip(offset).limit(limit).map(this::publicView).toList();
    }
    @PutMapping("/resources/{id}/status") public Object resourceStatus(@PathVariable UUID id,@Valid @RequestBody Status body) {
        if(!Set.of("AVAILABLE","UNAVAILABLE").contains(body.status()))throw ApiException.conflict("Only unassigned resource availability can be changed");
        return store.transaction(id,db->{
            var r=one(db,"SELECT * FROM resources WHERE id=? FOR UPDATE",id);
            if(r.get("reserved_for_request_id")!=null)throw ApiException.conflict("Resource belongs to an assignment");
            db.update("UPDATE resources SET status=?,available=?,version=version+1,updated_at=now() WHERE id=?",body.status(),body.status().equals("AVAILABLE"),id);
            return view(one(db,"SELECT * FROM resources WHERE id=?",id));
        });
    }
    @PutMapping("/requests/{id}/status") public Object status(@PathVariable UUID id,@Valid @RequestBody Status body) {
        return store.transaction(id,db->{
            var r=request(db,id,true);String before=r.get("status").toString(),after=body.status();
            if(before.equals(after))return publicView(r);
            if(!(before.equals("PENDING")&&after.equals("CANCELLED")||before.equals("MATCHED")&&after.equals("IN_PROGRESS")||before.equals("IN_PROGRESS")&&after.equals("COMPLETED")))throw ApiException.conflict("Invalid status transition");
            if(after.equals("COMPLETED")) {
                UUID resource=(UUID)r.get("matched_resource_id");
                store.owner(resource).update("UPDATE resources SET status='UNAVAILABLE',available=false,reserved_for_request_id=NULL,version=version+1,updated_at=now() WHERE id=? AND reserved_for_request_id=?",resource,id);
                db.update("UPDATE assignments SET state='COMPLETED',updated_at=now() WHERE request_id=?",id);
            }
            db.update("UPDATE emergency_requests SET status=?,version=version+1,updated_at=now() WHERE id=?",after,id);
            return publicView(request(db,id,false));
        });
    }
    @PostMapping("/requests/{id}/retry") public Object retry(@PathVariable UUID id) {
        return store.transaction(id,db->{
            var r=request(db,id,true);String status=r.get("status").toString();
            if(status.equals("NO_RESOURCE")) {db.update("UPDATE emergency_requests SET status='PENDING',updated_at=now(),version=version+1 WHERE id=?",id);event(db,id);}
            else if(!Set.of("PENDING","MATCHING","MATCHED","IN_PROGRESS","COMPLETED").contains(status))throw ApiException.conflict("Request cannot be matched");
            return Map.of("id",id,"status",Set.of("NO_RESOURCE","PENDING").contains(status)?"MATCH_QUEUED":status);
        });
    }
    @PostMapping("/requests/{id}/claim") public Object claim(@PathVariable UUID id) {
        return store.transaction(id,db->{
            var r=request(db,id,true);String status=r.get("status").toString();
            if(!Set.of("PENDING","MATCHING").contains(status))return Map.of("done",true);
            Boolean active=db.queryForObject("SELECT lease_until>now() FROM emergency_requests WHERE id=?",Boolean.class,id);
            if(Boolean.TRUE.equals(active))return Map.of("busy",true);
            UUID token=UUID.randomUUID();
            db.update("UPDATE emergency_requests SET status='MATCHING',lease_token=?,lease_until=now()+interval '60 seconds',updated_at=now() WHERE id=?",token,id);
            return view(request(db,id,false));
        });
    }
    private Map<String,Object> owned(JdbcTemplate db,UUID id,UUID token) {
        var r=request(db,id,true);
        if(!"MATCHING".equals(r.get("status"))||!token.equals(r.get("lease_token")))throw ApiException.conflict("Worker ownership changed");
        if(!Boolean.TRUE.equals(db.queryForObject("SELECT lease_until>now() FROM emergency_requests WHERE id=?",Boolean.class,id)))throw ApiException.conflict("Worker lease expired");
        return r;
    }
    @PostMapping("/requests/{id}/select") public Object select(@PathVariable UUID id,@Valid @RequestBody Selection body) {
        return store.transaction(id,db->{var r=owned(db,id,body.token());
            if(r.get("candidate_resource_id")!=null&&!body.resourceId().equals(r.get("candidate_resource_id")))throw ApiException.conflict("Resolve existing selection first");
            db.update("UPDATE emergency_requests SET candidate_resource_id=?,lease_until=now()+interval '60 seconds' WHERE id=?",body.resourceId(),id);return Map.of("selected",true);
        });
    }
    @PostMapping("/requests/{id}/commit") public Object commit(@PathVariable UUID id,@Valid @RequestBody Lease body) {
        return store.transaction(id,db->{
            var r=owned(db,id,body.token());UUID resource=(UUID)r.get("candidate_resource_id");
            if(resource==null)throw ApiException.conflict("No selected resource");
            // The request lock serializes stale workers. Selection was committed separately,
            // so a crash after the remote write retains a durable recovery pointer.
            var remote=store.owner(resource);
            int claimed=remote.update("UPDATE resources SET status='ALLOCATED',available=false,reserved_for_request_id=?,version=version+1,updated_at=now() WHERE id=? AND type=? AND ((status='AVAILABLE' AND available=true) OR (status='ALLOCATED' AND reserved_for_request_id=?))",id,resource,r.get("type"),id);
            if(claimed==0) {db.update("UPDATE emergency_requests SET candidate_resource_id=NULL WHERE id=?",id);return Map.of("matched",false);}
            db.update("INSERT INTO assignments(request_id,resource_id,state) VALUES (?,?,'COMMITTED') ON CONFLICT (request_id) DO NOTHING",id,resource);
            db.update("UPDATE emergency_requests SET status='MATCHED',matched_resource_id=?,candidate_resource_id=NULL,lease_token=NULL,lease_until=NULL,updated_at=now(),version=version+1 WHERE id=?",resource,id);
            return Map.of("matched",true,"resourceId",resource);
        });
    }
    @PostMapping("/requests/{id}/no-resource") public Object noResource(@PathVariable UUID id,@Valid @RequestBody Lease body) {
        return store.transaction(id,db->{var r=owned(db,id,body.token());if(r.get("candidate_resource_id")!=null)throw ApiException.conflict("Unresolved selection");
            db.update("UPDATE emergency_requests SET status='NO_RESOURCE',lease_token=NULL,lease_until=NULL,updated_at=now(),version=version+1 WHERE id=?",id);return Map.of("status","NO_RESOURCE");});
    }
    @GetMapping("/resources/candidates") public Object candidates(@RequestParam Type type) {
        var rows=new ArrayList<Map<String,Object>>();
        for(int i=0;i<3;i++)rows.addAll(store.shard(i).queryForList("SELECT * FROM resources WHERE type=? AND status='AVAILABLE' AND available=true",type.name()));
        return rows.stream().map(StorageController::view).toList();
    }
    @GetMapping("/recoverable") public Object recoverable() {
        var rows=new ArrayList<Map<String,Object>>();
        for(int i=0;i<3;i++)rows.addAll(store.shard(i).queryForList("SELECT id FROM emergency_requests WHERE status='MATCHING' AND lease_until<now() ORDER BY updated_at LIMIT 50"));
        return rows;
    }
    @GetMapping("/outbox/claim") public Object outbox() {
        var result=new ArrayList<Map<String,Object>>();
        for(int i=0;i<3;i++) {
            UUID token=UUID.randomUUID();
            var rows=store.shard(i).queryForList("WITH batch AS (SELECT id FROM outbox_events WHERE state='NEW' AND (lease_until IS NULL OR lease_until<now()) ORDER BY created_at FOR UPDATE SKIP LOCKED LIMIT 3) UPDATE outbox_events e SET lease_token=?,lease_until=now()+interval '60 seconds',attempts=attempts+1 FROM batch WHERE e.id=batch.id RETURNING e.id,e.request_id",token);
            for(var row:rows)result.add(Map.of("id",row.get("id"),"requestId",row.get("request_id"),"token",token));
        }
        return result;
    }
    @PostMapping("/outbox/{id}/published") public Object published(@PathVariable UUID id,@Valid @RequestBody PublishMark body) {
        int n=store.owner(body.requestId()).update("UPDATE outbox_events SET state='PUBLISHED',published_at=now(),lease_until=NULL WHERE id=? AND request_id=? AND lease_token=? AND state='NEW'",id,body.requestId(),body.token());
        if(n==0)throw ApiException.conflict("Outbox lease changed");return Map.of("published",true);
    }
    @DeleteMapping("/data/requests/{id}") public Object deleteRequest(@PathVariable UUID id) {
        return store.transaction(id,db->{
            var row=request(db,id,true);
            if(!"CANCELLED".equals(row.get("status")))throw ApiException.conflict("Only cancelled requests may be deleted");
            db.update("DELETE FROM outbox_events WHERE request_id=?",id);
            db.update("DELETE FROM emergency_requests WHERE id=?",id);
            return Map.of("deleted",true);
        });
    }
    @DeleteMapping("/data/resources/{id}") public Object deleteResource(@PathVariable UUID id) {
        return store.transaction(id,db->{
            var row=one(db,"SELECT * FROM resources WHERE id=? FOR UPDATE",id);
            // Keep referenced historical resources; only never-allocated unavailable rows qualify.
            if(!"UNAVAILABLE".equals(row.get("status"))||((Number)row.get("version")).longValue()!=0)throw ApiException.conflict("Only unused unavailable resources may be deleted");
            db.update("DELETE FROM resources WHERE id=?",id);return Map.of("deleted",true);
        });
    }
}
