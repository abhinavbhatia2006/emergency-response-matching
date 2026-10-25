package com.example.emergency.common;
import jakarta.servlet.http.HttpServletRequest;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.*;
import org.springframework.web.bind.MethodArgumentNotValidException;
import org.springframework.web.client.RestClientResponseException;
import java.time.Instant;
import java.util.Map;
@RestControllerAdvice
public class Errors {
 @ExceptionHandler(ApiException.class) ResponseEntity<?> domain(ApiException e,HttpServletRequest r){return error(e.status,e.code,e.getMessage(),r);}
 @ExceptionHandler({MethodArgumentNotValidException.class,org.springframework.http.converter.HttpMessageNotReadableException.class,org.springframework.web.method.annotation.MethodArgumentTypeMismatchException.class,IllegalArgumentException.class})
 ResponseEntity<?> bad(Exception e,HttpServletRequest r){return error(400,"INVALID_INPUT","Check types, coordinates, required fields and allowed values",r);}
 @ExceptionHandler(RestClientResponseException.class) ResponseEntity<?> remote(RestClientResponseException e,HttpServletRequest r){int s=e.getStatusCode().value();return error(s==400?400:s==404?404:s==409?409:503,"UPSTREAM_ERROR",s==400?"Invalid request parameters":s==404?"Record not found":s==409?"Operation conflicts with current state":"Storage is temporarily unavailable",r);}
 @ExceptionHandler(org.springframework.web.ErrorResponseException.class) ResponseEntity<?> http(org.springframework.web.ErrorResponseException e,HttpServletRequest r){return error(e.getStatusCode().value(),"HTTP_ERROR","Request cannot be handled with this method, path or content type",r);}
 @ExceptionHandler({org.springframework.web.HttpRequestMethodNotSupportedException.class,org.springframework.web.HttpMediaTypeNotSupportedException.class,org.springframework.web.HttpMediaTypeNotAcceptableException.class,org.springframework.web.servlet.resource.NoResourceFoundException.class,org.springframework.web.bind.MissingServletRequestParameterException.class})
 ResponseEntity<?> protocol(Exception e,HttpServletRequest r){var response=(org.springframework.web.ErrorResponse)e;return error(response.getStatusCode().value(),"HTTP_ERROR","Check the request method, path, content type and parameters",r);}
 @ExceptionHandler(Exception.class) ResponseEntity<?> unavailable(Exception e,HttpServletRequest r){org.slf4j.LoggerFactory.getLogger(Errors.class).warn("operation_failed class={}",e.getClass().getSimpleName());return error(503,"TEMPORARILY_UNAVAILABLE","Service temporarily unavailable; retry later",r);}
 private ResponseEntity<?> error(int s,String c,String m,HttpServletRequest r){return ResponseEntity.status(s).body(Map.of("timestamp",Instant.now().toString(),"status",s,"code",c,"message",m,"path",r.getRequestURI(),"requestId",String.valueOf(r.getAttribute("traceId"))));}
}
