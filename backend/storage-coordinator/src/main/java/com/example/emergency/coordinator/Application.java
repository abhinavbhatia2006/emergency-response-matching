package com.example.emergency.coordinator;
import org.springframework.boot.SpringApplication;
import org.springframework.boot.autoconfigure.SpringBootApplication;
import org.springframework.scheduling.annotation.EnableScheduling;
@SpringBootApplication(scanBasePackages={"com.example.emergency.coordinator","com.example.emergency.common"})
@EnableScheduling
public class Application { public static void main(String[] args) { SpringApplication.run(Application.class,args); } }
