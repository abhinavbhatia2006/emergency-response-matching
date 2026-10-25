package com.example.emergency.common;
import org.junit.jupiter.api.Test;
import org.springframework.mock.web.MockHttpServletRequest;
import org.springframework.web.client.HttpClientErrorException;
import org.springframework.http.HttpStatus;
import static org.junit.jupiter.api.Assertions.*;
class ErrorsTest {
 @Test void preservesInvalidInputAndMissingRecordWhileSanitizingUpstreamFailure(){
  var errors=new Errors();var request=new MockHttpServletRequest();
  for(var status:new HttpStatus[]{HttpStatus.BAD_REQUEST,HttpStatus.NOT_FOUND,HttpStatus.CONFLICT})
   assertEquals(status,errors.remote(new HttpClientErrorException(status),request).getStatusCode());
  assertEquals(HttpStatus.SERVICE_UNAVAILABLE,errors.remote(new HttpClientErrorException(HttpStatus.FORBIDDEN),request).getStatusCode());
 }
 @Test void protocolErrorsRetainTheirHttpMeaning(){
  var errors=new Errors();var request=new MockHttpServletRequest();
  assertEquals(406,errors.protocol(new org.springframework.web.HttpMediaTypeNotAcceptableException("Unsupported response format"),request).getStatusCode().value());
  assertEquals(405,errors.protocol(new org.springframework.web.HttpRequestMethodNotSupportedException("DELETE"),request).getStatusCode().value());
  assertEquals(400,errors.protocol(new org.springframework.web.bind.MissingServletRequestParameterException("type","String"),request).getStatusCode().value());
 }
}
