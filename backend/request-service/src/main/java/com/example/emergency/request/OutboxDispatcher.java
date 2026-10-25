package com.example.emergency.request;
import com.example.emergency.common.StorageClient;
import org.springframework.stereotype.Component;
import org.springframework.scheduling.annotation.Scheduled;
import org.springframework.amqp.rabbit.core.RabbitTemplate;
import org.springframework.amqp.rabbit.connection.CorrelationData;
import org.springframework.amqp.core.*;
import tools.jackson.databind.json.JsonMapper;
import java.util.*;
import java.util.concurrent.TimeUnit;
@Component
public class OutboxDispatcher {
 private final StorageClient c;private final RabbitTemplate rabbit;private final JsonMapper json=JsonMapper.builder().build();
 private final org.slf4j.Logger log=org.slf4j.LoggerFactory.getLogger(getClass());
 public OutboxDispatcher(StorageClient c,RabbitTemplate rabbit){this.c=c;this.rabbit=rabbit;}
 @Scheduled(fixedDelayString="${emergency.outbox-delay-ms:1000}") void publish(){
  try{for(var event:c.list("/internal/outbox/claim")){
   try{
    var correlation=new CorrelationData(event.get("id").toString());
    var properties=new MessageProperties();properties.setContentType("application/json");properties.setDeliveryMode(MessageDeliveryMode.PERSISTENT);properties.setMessageId(event.get("id").toString());
    rabbit.send("emergency.events","request.created",new Message(json.writeValueAsBytes(Map.of("eventId",event.get("id"),"requestId",event.get("requestId"))),properties),correlation);
    var confirm=correlation.getFuture().get(5,TimeUnit.SECONDS);
    if(!confirm.isAck()||correlation.getReturned()!=null)throw new IllegalStateException("Unconfirmed or unroutable event");
    c.post("/internal/outbox/"+event.get("id")+"/published",Map.of("requestId",event.get("requestId"),"token",event.get("token")));
   }catch(Exception e){log.warn("outbox_retry eventId={} error={}",event.get("id"),e.getClass().getSimpleName());}
  }}catch(Exception e){log.warn("outbox_scan_unavailable error={}",e.getClass().getSimpleName());}
 }
}
