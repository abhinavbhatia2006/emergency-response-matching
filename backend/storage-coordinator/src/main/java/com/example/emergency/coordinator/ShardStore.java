package com.example.emergency.coordinator;

import com.zaxxer.hikari.HikariConfig;
import com.zaxxer.hikari.HikariDataSource;
import jakarta.annotation.PreDestroy;
import org.flywaydb.core.Flyway;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.jdbc.core.JdbcTemplate;
import org.springframework.jdbc.support.JdbcTransactionManager;
import org.springframework.stereotype.Component;
import org.springframework.transaction.support.TransactionTemplate;
import java.util.*;
import java.util.function.Function;

@Component
public class ShardStore {
    private final List<HikariDataSource> pools = new ArrayList<>();
    private final List<JdbcTemplate> queries = new ArrayList<>();
    private final List<TransactionTemplate> transactions = new ArrayList<>();
    private final List<JdbcTemplate> replicas = new ArrayList<>();

    public ShardStore(@Value("${emergency.shard-urls}") String urls,
                      @Value("${emergency.db-user}") String user,
                      @Value("${emergency.db-password}") String password,
                      @Value("${emergency.read-urls:}") String readUrls) {
        String[] addresses = urls.split(",");
        if (addresses.length != 3) throw new IllegalArgumentException("Exactly three shard URLs required");
        try {
            for (int i = 0; i < 3; i++) {
                var config = new HikariConfig();
                config.setJdbcUrl(addresses[i].trim());
                config.setUsername(user);
                config.setPassword(password);
                config.setPoolName("shard-" + i);
                config.setMaximumPoolSize(8);
                config.setMinimumIdle(1);
                config.setConnectionTimeout(3000);
                config.setValidationTimeout(2000);
                config.addDataSourceProperty("connectTimeout", "3");
                config.addDataSourceProperty("socketTimeout", "10");
                var pool = new HikariDataSource(config);
                pools.add(pool);
                Flyway.configure().dataSource(pool).locations("classpath:db/migration").load().migrate();
                var jdbc = new JdbcTemplate(pool);
                jdbc.setQueryTimeout(10);
                queries.add(jdbc);
                var tx = new TransactionTemplate(new JdbcTransactionManager(pool));
                tx.setTimeout(20);
                transactions.add(tx);
            }
            if (!readUrls.isBlank()) {
                String[] reads = readUrls.split(",");
                if (reads.length != 3) throw new IllegalArgumentException("Exactly three read URLs required");
                for (int i=0;i<3;i++) {
                    var config=new HikariConfig();
                    config.setJdbcUrl(reads[i].trim());config.setUsername(user);config.setPassword(password);
                    config.setPoolName("replica-"+i);config.setMaximumPoolSize(3);config.setMinimumIdle(0);
                    config.setReadOnly(true);config.setInitializationFailTimeout(-1);config.setConnectionTimeout(2000);
                    config.addDataSourceProperty("connectTimeout","2");config.addDataSourceProperty("socketTimeout","5");
                    var pool=new HikariDataSource(config);pools.add(pool);
                    var query=new JdbcTemplate(pool);query.setQueryTimeout(5);replicas.add(query);
                }
            }
        } catch (RuntimeException e) { close(); throw e; }
    }
    public JdbcTemplate shard(int index) { return queries.get(index); }
    public JdbcTemplate owner(UUID id) { return shard(ShardRouter.shardFor(id)); }
    /** Replica fallback is only for public read-only views, never authoritative matching decisions. */
    public Map<String,Object> read(UUID id, Function<JdbcTemplate,Map<String,Object>> operation) {
        try { return operation.apply(owner(id)); }
        catch (org.springframework.dao.DataAccessException failure) {
            if(replicas.isEmpty())throw failure;
            var result=new LinkedHashMap<>(operation.apply(replicas.get(ShardRouter.shardFor(id))));
            result.put("readSource","REPLICA");result.put("potentiallyStale",true);return result;
        }
    }
    public <T> T transaction(UUID id, Function<JdbcTemplate,T> operation) {
        int index = ShardRouter.shardFor(id);
        return transactions.get(index).execute(status -> operation.apply(shard(index)));
    }
    @PreDestroy public void close() { pools.forEach(HikariDataSource::close); }
}
