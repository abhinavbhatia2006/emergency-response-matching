CREATE TABLE emergency_requests (
    id UUID PRIMARY KEY,
    type VARCHAR(20) NOT NULL CHECK (type IN ('WATER','FOOD','MEDICINE','TRANSPORT','SHELTER')),
    location_text VARCHAR(200) NOT NULL,
    latitude DOUBLE PRECISION NOT NULL CHECK (latitude BETWEEN -90 AND 90),
    longitude DOUBLE PRECISION NOT NULL CHECK (longitude BETWEEN -180 AND 180),
    details VARCHAR(2000) NOT NULL,
    priority VARCHAR(10) NOT NULL DEFAULT 'NORMAL' CHECK (priority IN ('NORMAL','HIGH','CRITICAL')),
    status VARCHAR(20) NOT NULL DEFAULT 'PENDING' CHECK (status IN ('PENDING','MATCHING','MATCHED','IN_PROGRESS','COMPLETED','NO_RESOURCE','CANCELLED')),
    matched_resource_id UUID,
    candidate_resource_id UUID,
    lease_token UUID,
    lease_until TIMESTAMPTZ,
    version BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE resources (
    id UUID PRIMARY KEY,
    type VARCHAR(20) NOT NULL CHECK (type IN ('WATER','FOOD','MEDICINE','TRANSPORT','SHELTER')),
    location_text VARCHAR(200) NOT NULL,
    latitude DOUBLE PRECISION NOT NULL CHECK (latitude BETWEEN -90 AND 90),
    longitude DOUBLE PRECISION NOT NULL CHECK (longitude BETWEEN -180 AND 180),
    description VARCHAR(2000) NOT NULL DEFAULT '',
    available BOOLEAN NOT NULL,
    status VARCHAR(20) NOT NULL CHECK (status IN ('AVAILABLE','RESERVED','ALLOCATED','UNAVAILABLE')),
    reserved_for_request_id UUID,
    version BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (available = (status = 'AVAILABLE')),
    CHECK ((status IN ('RESERVED','ALLOCATED')) = (reserved_for_request_id IS NOT NULL))
);
CREATE TABLE assignments (
    request_id UUID PRIMARY KEY REFERENCES emergency_requests(id),
    resource_id UUID NOT NULL,
    state VARCHAR(20) NOT NULL CHECK (state IN ('COMMITTED','COMPLETED')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE outbox_events (
    id UUID PRIMARY KEY,
    request_id UUID NOT NULL REFERENCES emergency_requests(id),
    state VARCHAR(20) NOT NULL DEFAULT 'NEW' CHECK (state IN ('NEW','PUBLISHED')),
    lease_token UUID,
    lease_until TIMESTAMPTZ,
    attempts INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at TIMESTAMPTZ
);
CREATE TABLE processed_events (
    consumer_name VARCHAR(100) NOT NULL,
    event_id UUID NOT NULL,
    processed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY(consumer_name,event_id)
);
CREATE INDEX idx_resources_candidates ON resources(type,status);
CREATE INDEX idx_requests_recovery ON emergency_requests(status,lease_until);
CREATE INDEX idx_outbox_pending ON outbox_events(created_at) WHERE state = 'NEW';
