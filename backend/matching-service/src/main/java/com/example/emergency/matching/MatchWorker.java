package com.example.emergency.matching;
import com.example.emergency.common.*;
import com.example.emergency.common.Contracts.Event;
import com.rabbitmq.client.Channel;
import org.springframework.amqp.core.Message;
import org.springframework.amqp.rabbit.annotation.RabbitListener;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.scheduling.annotation.Scheduled;
import org.springframework.stereotype.Component;
import tools.jackson.databind.json.JsonMapper;
import java.util.*;

@Component
public class MatchWorker {
    private final StorageClient client;
    private final double radius;
    private final JsonMapper json=JsonMapper.builder().build();
    private final org.slf4j.Logger log=org.slf4j.LoggerFactory.getLogger(getClass());
    public MatchWorker(StorageClient client,@Value("${emergency.match-radius-km:50}") double radius) {this.client=client;this.radius=radius;}
    @RabbitListener(queues="emergency.match") public void receive(Message message,Channel channel)throws Exception {
        long tag=message.getMessageProperties().getDeliveryTag();Event event;
        try {event=json.readValue(message.getBody(),Event.class);if(event.eventId()==null||event.requestId()==null)throw new IllegalArgumentException();}
        catch(Exception e){channel.basicReject(tag,false);return;}
        try {match(event.requestId());channel.basicAck(tag,false);}
        catch(Exception e) {
            if(e instanceof org.springframework.web.client.RestClientResponseException response &&
                (response.getStatusCode().value()==400||response.getStatusCode().value()==404)) {
                log.warn("match_rejected requestId={} eventId={}",event.requestId(),event.eventId());
                channel.basicReject(tag,false);return;
            }
            // Requeue transient failures. Consumer backoff bounds broker churn; durable
            // request leases and reconciliation recover crashes after work was claimed.
            log.warn("match_retry requestId={} eventId={} error={}",event.requestId(),event.eventId(),e.getClass().getSimpleName());
            Thread.sleep(1000);channel.basicNack(tag,false,true);
        }
    }
    public void match(UUID id) {
        String base="/internal/requests/"+id;
        var request=client.post(base+"/claim",Map.of());
        if(Boolean.TRUE.equals(request.get("done"))||Boolean.TRUE.equals(request.get("busy")))return;
        var lease=Map.of("token",request.get("leaseToken"));
        if(request.get("candidateResourceId")!=null&&Boolean.TRUE.equals(client.post(base+"/commit",lease).get("matched")))return;
        var candidates=MatchingPolicy.rank(request,client.list("/internal/resources/candidates?type="+request.get("type")),radius);
        for(var resource:candidates) {
            client.post(base+"/select",Map.of("token",request.get("leaseToken"),"resourceId",resource.get("id")));
            if(Boolean.TRUE.equals(client.post(base+"/commit",lease).get("matched"))) {log.info("match_committed requestId={} resourceId={}",id,resource.get("id"));return;}
        }
        client.post(base+"/no-resource",lease);
    }
    @Scheduled(fixedDelayString="${emergency.reconcile-delay-ms:15000}") public void reconcile() {
        try {for(var row:client.list("/internal/recoverable"))try {match(UUID.fromString(row.get("id").toString()));}catch(Exception e){log.warn("reconcile_retry requestId={}",row.get("id"));}}
        catch(Exception e){log.warn("reconcile_scan_unavailable");}
    }
}
