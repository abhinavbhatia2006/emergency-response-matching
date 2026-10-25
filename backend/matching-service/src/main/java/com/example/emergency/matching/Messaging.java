package com.example.emergency.matching;
import org.springframework.context.annotation.*;
import org.springframework.amqp.core.*;
@Configuration
public class Messaging {
 @Bean Declarables topology(){
  var exchange=new DirectExchange("emergency.events",true,false);
  var dead=new DirectExchange("emergency.dead",true,false);
  var queue=QueueBuilder.durable("emergency.match").deadLetterExchange("emergency.dead").deadLetterRoutingKey("failed").build();
  var dlq=QueueBuilder.durable("emergency.dead").build();
  return new Declarables(exchange,dead,queue,dlq,BindingBuilder.bind(queue).to(exchange).with("request.created"),BindingBuilder.bind(dlq).to(dead).with("failed"));
 }
}
