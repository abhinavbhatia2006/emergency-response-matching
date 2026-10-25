package com.example.emergency.common;
import jakarta.validation.Valid;
import jakarta.validation.constraints.*;
import java.util.UUID;
public final class Contracts {
 private Contracts() {}
 public enum Type { WATER, FOOD, MEDICINE, TRANSPORT, SHELTER }
 public record Location(@NotBlank @Size(max=200) String label, @NotNull @DecimalMin("-90") @DecimalMax("90") Double latitude, @NotNull @DecimalMin("-180") @DecimalMax("180") Double longitude) {}
 public record NewRequest(@NotNull Type type,@NotNull @Valid Location location,@NotBlank @Size(max=2000) String details,@Pattern(regexp="NORMAL|HIGH|CRITICAL") String priority) {}
 public record NewResource(@NotNull Type type,@NotNull @Valid Location location,@Size(max=2000) String description,@NotNull Boolean availability) {}
 public record Status(@NotBlank String status) {}
 public record Selection(@NotNull UUID token,@NotNull UUID resourceId) {}
 public record Lease(@NotNull UUID token) {}
 public record Event(UUID eventId,UUID requestId) {}
 public record PublishMark(@NotNull UUID requestId,@NotNull UUID token) {}
 public record Node(@NotBlank @Size(max=100) String nodeId,@Min(0) @Max(2) int shard,@NotNull @Pattern(regexp="PRIMARY|STANDBY|UNKNOWN") String role,boolean healthy,long lagBytes) {}
}
