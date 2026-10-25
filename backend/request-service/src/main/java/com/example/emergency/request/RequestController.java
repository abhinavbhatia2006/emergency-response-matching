package com.example.emergency.request;
import com.example.emergency.common.*;
import com.example.emergency.common.Contracts.*;
import jakarta.validation.Valid;
import org.springframework.web.bind.annotation.*;
import org.springframework.http.ResponseEntity;
import java.net.URI;
import java.util.*;
@RestController
public class RequestController {
 private final StorageClient c;
 public RequestController(StorageClient c){this.c=c;}
 @PostMapping("/requests") ResponseEntity<?> create(@Valid @RequestBody NewRequest body){var v=c.post("/internal/requests",body);return ResponseEntity.created(URI.create("/requests/"+v.get("id"))).body(v);}
 @GetMapping("/requests/{id}") Object get(@PathVariable UUID id){return c.get("/internal/requests/"+id);}
 @GetMapping("/requests") Object list(@RequestParam(defaultValue="0") int offset,@RequestParam(defaultValue="50") int limit){return c.list("/internal/requests?offset="+offset+"&limit="+limit);}
 @PutMapping("/requests/{id}/status") Object status(@PathVariable UUID id,@Valid @RequestBody Status body){return c.put("/internal/requests/"+id+"/status",body);}
 @PostMapping("/match/{id}") Object match(@PathVariable UUID id){return c.post("/internal/requests/"+id+"/retry",Map.of());}
 @GetMapping("/ops/topology") Object topology(){return c.get("/internal/topology");}
}
