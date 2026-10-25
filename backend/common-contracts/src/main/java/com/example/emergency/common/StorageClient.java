package com.example.emergency.common;
import org.springframework.stereotype.Component;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.web.client.RestClient;
import org.springframework.http.client.JdkClientHttpRequestFactory;
import java.net.http.HttpClient;
import java.time.Duration;
import java.util.*;
@Component
public class StorageClient {
 private final RestClient client;
 public StorageClient(@Value("${emergency.coordinator-url}") String url,@Value("${emergency.internal-token}") String token){
  var f=new JdkClientHttpRequestFactory(HttpClient.newBuilder().connectTimeout(Duration.ofSeconds(3)).build());f.setReadTimeout(Duration.ofSeconds(15));
  client=RestClient.builder().baseUrl(url).requestFactory(f).defaultHeader("X-Internal-Token",token).requestInterceptor((r,b,e)->{String trace=org.slf4j.MDC.get("traceId");if(trace!=null)r.getHeaders().set("X-Correlation-ID",trace);return e.execute(r,b);}).build();
 }
 @SuppressWarnings("unchecked") public Map<String,Object> get(String path){return client.get().uri(path).retrieve().body(Map.class);}
 @SuppressWarnings("unchecked") public List<Map<String,Object>> list(String path){return client.get().uri(path).retrieve().body(List.class);}
 @SuppressWarnings("unchecked") public Map<String,Object> post(String path,Object value){return client.post().uri(path).body(value).retrieve().body(Map.class);}
 @SuppressWarnings("unchecked") public Map<String,Object> put(String path,Object value){return client.put().uri(path).body(value).retrieve().body(Map.class);}
}
