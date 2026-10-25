package com.example.emergency.common;
import jakarta.servlet.*;
import jakarta.servlet.http.*;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.stereotype.Component;
import org.springframework.web.filter.OncePerRequestFilter;
import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.util.UUID;
import org.slf4j.MDC;
@Component
public class BoundaryFilter extends OncePerRequestFilter {
 private final String token;
 public BoundaryFilter(@Value("${emergency.internal-token}") String token){if(token.length()<24)throw new IllegalArgumentException("INTERNAL_TOKEN must have at least 24 characters");this.token=token;}
 protected void doFilterInternal(HttpServletRequest r,HttpServletResponse s,FilterChain chain)throws ServletException,IOException {
  String trace=r.getHeader("X-Correlation-ID"); if(trace==null||!trace.matches("[a-zA-Z0-9-]{1,64}"))trace=UUID.randomUUID().toString();
  r.setAttribute("traceId",trace);s.setHeader("X-Correlation-ID",trace);MDC.put("traceId",trace);
  try {
   if(r.getRequestURI().startsWith("/internal/")&&!MessageDigest.isEqual(token.getBytes(StandardCharsets.UTF_8),String.valueOf(r.getHeader("X-Internal-Token")).getBytes(StandardCharsets.UTF_8))){s.sendError(403);return;}
   if(r.getContentLengthLong()>32768){s.sendError(413);return;}
   chain.doFilter(r,s);
  }finally{MDC.remove("traceId");}
 }
}
