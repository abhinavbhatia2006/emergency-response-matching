package com.example.emergency.resource;
import com.example.emergency.common.*;
import com.example.emergency.common.Contracts.*;
import jakarta.validation.Valid;
import org.springframework.web.bind.annotation.*;
import org.springframework.http.ResponseEntity;
import java.net.URI;
import java.util.UUID;
@RestController
public class ResourceController {
 private final StorageClient c;
 public ResourceController(StorageClient c){this.c=c;}
 @PostMapping("/resources") ResponseEntity<?> create(@Valid @RequestBody NewResource body){var v=c.post("/internal/resources",body);return ResponseEntity.created(URI.create("/resources/"+v.get("id"))).body(v);}
 @GetMapping("/resources") Object list(@RequestParam(required=false) Type type,@RequestParam(defaultValue="0") int offset,@RequestParam(defaultValue="50") int limit){return c.list("/internal/resources?offset="+offset+"&limit="+limit+(type==null?"":"&type="+type));}
 @GetMapping("/resources/{id}") Object get(@PathVariable UUID id){return c.get("/internal/resources/"+id);}
 @PutMapping("/resources/{id}/status") Object status(@PathVariable UUID id,@Valid @RequestBody Status body){return c.put("/internal/resources/"+id+"/status",body);}
}
