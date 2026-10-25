FROM maven:3.9.11-eclipse-temurin-21 AS build
WORKDIR /build
COPY pom.xml ./
COPY backend ./backend
ARG SERVICE
RUN mvn -B -q -pl backend/${SERVICE} -am package -DskipTests
RUN cp backend/${SERVICE}/target/${SERVICE}-1.0.0-SNAPSHOT.jar /app.jar
FROM eclipse-temurin:21-jre
WORKDIR /app
RUN groupadd --gid 10001 app && useradd --uid 10001 --gid app app
COPY --from=build /app.jar /app/app.jar
USER 10001
EXPOSE 8080
ENV JAVA_TOOL_OPTIONS="-XX:MaxRAMPercentage=70 -XX:+ExitOnOutOfMemoryError"
ENTRYPOINT ["java","-jar","/app/app.jar"]
