package application

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/abhinavbhatia2006/emergency-response-matching/internal/domain"
	"github.com/abhinavbhatia2006/emergency-response-matching/internal/storage"
)

var ErrUnavailable = errors.New("service unavailable; verify current state before retrying")

type Service struct {
	base, token       string
	client            *http.Client
	maxDistanceMeters float64
}

type Option func(*Service)

func WithMaxDistanceMeters(limit float64) Option {
	return func(s *Service) { s.maxDistanceMeters = limit }
}
func New(base, token string, options ...Option) (*Service, error) {
	u, e := url.Parse(base)
	if e != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || strings.TrimSpace(token) == "" || strings.ContainsAny(token, "\r\n") {
		return nil, domain.ErrInvalid
	}
	s := &Service{base: strings.TrimRight(base, "/"), token: token, maxDistanceMeters: 50000, client: &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirects disabled") }}}
	for _, option := range options {
		if option == nil {
			return nil, domain.ErrInvalid
		}
		option(s)
	}
	if math.IsNaN(s.maxDistanceMeters) || math.IsInf(s.maxDistanceMeters, 0) || s.maxDistanceMeters <= 0 || s.maxDistanceMeters > 20015000 {
		return nil, domain.ErrInvalid
	}
	return s, nil
}
func (s *Service) call(ctx context.Context, method, path string, input, output any) error {
	var body io.Reader
	if input != nil {
		b, e := json.Marshal(input)
		if e != nil {
			return domain.ErrInvalid
		}
		body = bytes.NewReader(b)
	}
	r, e := http.NewRequestWithContext(ctx, method, s.base+path, body)
	if e != nil {
		return e
	}
	r.Header.Set("Authorization", "Bearer "+s.token)
	r.Header.Set("Content-Type", "application/json")
	resp, e := s.client.Do(r)
	if e != nil {
		return fmt.Errorf("%w: coordinator connection", ErrUnavailable)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case 200:
	case 400, 413:
		return domain.ErrInvalid
	case 404:
		return storage.ErrNotFound
	case 409:
		return storage.ErrConflict
	default:
		return ErrUnavailable
	}
	data, e := io.ReadAll(io.LimitReader(resp.Body, 32<<20+1))
	if e != nil || len(data) > 32<<20 {
		return ErrUnavailable
	}
	if output != nil && json.Unmarshal(data, output) != nil {
		return ErrUnavailable
	}
	return nil
}
func (s *Service) records(ctx context.Context) ([]storage.Record, error) {
	var records []storage.Record
	e := s.call(ctx, "GET", "/internal/data", nil, &records)
	return records, e
}
func (s *Service) batch(ctx context.Context, ms []storage.Mutation) error {
	var records []storage.Record
	return s.call(ctx, "POST", "/internal/batch", struct {
		Mutations []storage.Mutation `json:"mutations"`
	}{ms}, &records)
}
func (s *Service) get(ctx context.Context, key string) (storage.Record, error) {
	var r storage.Record
	e := s.call(ctx, "GET", "/internal/data/"+url.PathEscape(key), nil, &r)
	return r, e
}
func validID(id string) bool {
	b, e := hex.DecodeString(id)
	return e == nil && len(b) == 16 && id == strings.ToLower(id)
}
func put(key string, v any, expected uint64) storage.Mutation {
	b, _ := json.Marshal(v)
	return storage.Mutation{Key: key, Value: b, Expected: expected}
}
func jobMutation(requestID string) (storage.Mutation, error) {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		return storage.Mutation{}, e
	}
	id := hex.EncodeToString(b[:])
	return put("job/"+id, Job{ID: id, RequestID: requestID}, 0), nil
}
func (s *Service) CreateRequest(ctx context.Context, kind, location, details string, coordinates ...*domain.Coordinates) (domain.Request, error) {
	point, e := copyCoordinates(coordinates)
	if e != nil {
		return domain.Request{}, e
	}
	return s.createRequest(ctx, kind, domain.LocationInput{Label: location, Coordinates: point}, details)
}

func (s *Service) createRequest(ctx context.Context, kind string, location domain.LocationInput, details string) (domain.Request, error) {
	r, e := domain.NewRequestAt(kind, location, details, time.Now())
	if e != nil {
		return r, e
	}
	j, e := jobMutation(r.ID)
	if e != nil {
		return domain.Request{}, e
	}
	e = s.batch(ctx, []storage.Mutation{put("request/"+r.ID, r, 0), j})
	return r, e
}
func (s *Service) GetRequest(ctx context.Context, id string) (domain.Request, error) {
	var r domain.Request
	if !validID(id) {
		return r, domain.ErrInvalid
	}
	record, e := s.get(ctx, "request/"+id)
	if e != nil {
		return r, e
	}
	if json.Unmarshal(record.Value, &r) != nil {
		return r, ErrUnavailable
	}
	return r, nil
}
func (s *Service) ListRequests(ctx context.Context) ([]domain.Request, error) {
	records, e := s.records(ctx)
	if e != nil {
		return nil, e
	}
	out := make([]domain.Request, 0)
	for _, r := range records {
		if strings.HasPrefix(r.Key, "request/") && !r.Deleted {
			var item domain.Request
			if json.Unmarshal(r.Value, &item) != nil {
				return nil, ErrUnavailable
			}
			out = append(out, item)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}
func (s *Service) ListResources(ctx context.Context) ([]domain.Resource, error) {
	records, e := s.records(ctx)
	if e != nil {
		return nil, e
	}
	out := make([]domain.Resource, 0)
	for _, r := range records {
		if strings.HasPrefix(r.Key, "resource/") && !r.Deleted {
			var item domain.Resource
			if json.Unmarshal(r.Value, &item) != nil {
				return nil, ErrUnavailable
			}
			out = append(out, item)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
func (s *Service) CreateResource(ctx context.Context, kind, location string, available bool, coordinates ...*domain.Coordinates) (domain.Resource, error) {
	point, e := copyCoordinates(coordinates)
	if e != nil {
		return domain.Resource{}, e
	}
	return s.createResource(ctx, kind, domain.LocationInput{Label: location, Coordinates: point}, available)
}

func (s *Service) createResource(ctx context.Context, kind string, location domain.LocationInput, available bool) (domain.Resource, error) {
	r, e := domain.NewResourceAt(kind, location, available)
	if e != nil {
		return r, e
	}
	ms := []storage.Mutation{put("resource/"+r.ID, r, 0)}
	if available {
		job, e := jobMutation("rescan")
		if e != nil {
			return domain.Resource{}, e
		}
		ms = append(ms, job)
	}
	e = s.batch(ctx, ms)
	return r, e
}
func retry(ctx context.Context, attempt int) error {
	t := time.NewTimer(time.Duration(attempt+1) * 5 * time.Millisecond)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
func (s *Service) Match(ctx context.Context, id string) (domain.Request, error) {
	if id == "rescan" {
		requests, e := s.ListRequests(ctx)
		if e != nil {
			return domain.Request{}, e
		}
		resources, e := s.ListResources(ctx)
		if e != nil {
			return domain.Request{}, e
		}
		for _, request := range requests {
			if request.Status != domain.Pending {
				continue
			}
			if request.Coordinates == nil {
				if _, e := domain.SelectResource(request, resources); errors.Is(e, domain.ErrNoMatch) {
					continue
				}
			}
			matched, e := s.Match(ctx, request.ID)
			if errors.Is(e, domain.ErrNoMatch) {
				continue
			}
			if e != nil {
				return domain.Request{}, e
			}
			for i := range resources {
				if resources[i].ID == matched.ResourceID {
					resources[i].Availability = false
					resources[i].Status = domain.Reserved
				}
			}
		}
		return domain.Request{}, nil
	}
	if !validID(id) {
		return domain.Request{}, domain.ErrInvalid
	}
	for attempt := 0; attempt < 16; attempt++ {
		records, e := s.records(ctx)
		if e != nil {
			return domain.Request{}, e
		}
		var req domain.Request
		var reqRecord storage.Record
		resources := []domain.Resource{}
		versions := map[string]uint64{}
		for _, r := range records {
			if r.Deleted {
				continue
			}
			if r.Key == "request/"+id {
				if json.Unmarshal(r.Value, &req) != nil {
					return req, ErrUnavailable
				}
				reqRecord = r
			}
			if strings.HasPrefix(r.Key, "resource/") {
				var item domain.Resource
				if json.Unmarshal(r.Value, &item) != nil {
					return req, ErrUnavailable
				}
				resources = append(resources, item)
				versions[item.ID] = r.Version
			}
		}
		if reqRecord.Version == 0 {
			return req, storage.ErrNotFound
		}
		if req.Status != domain.Pending {
			return req, nil
		}
		resource, distance, e := s.selectResource(req, resources)
		if e != nil {
			return req, e
		}
		req.ResourceID = resource.ID
		req.DistanceMeters = distance
		if e = req.Transition(domain.Matched); e != nil {
			return req, e
		}
		resource.Status = domain.Reserved
		resource.Availability = false
		e = s.batch(ctx, []storage.Mutation{put(reqRecord.Key, req, reqRecord.Version), put("resource/"+resource.ID, resource, versions[resource.ID])})
		if !errors.Is(e, storage.ErrConflict) {
			return req, e
		}
		if e = retry(ctx, attempt); e != nil {
			return domain.Request{}, e
		}
	}
	return domain.Request{}, storage.ErrConflict
}
func (s *Service) UpdateRequest(ctx context.Context, id, status string) (domain.Request, error) {
	if !validID(id) || status == string(domain.Matched) {
		return domain.Request{}, domain.ErrInvalid
	}
	for attempt := 0; attempt < 16; attempt++ {
		rec, e := s.get(ctx, "request/"+id)
		if e != nil {
			return domain.Request{}, e
		}
		var req domain.Request
		if json.Unmarshal(rec.Value, &req) != nil {
			return req, ErrUnavailable
		}
		previous := req.Status
		if e = req.Transition(domain.RequestStatus(status)); e != nil {
			return req, e
		}
		if previous == req.Status {
			return req, nil
		}
		ms := []storage.Mutation{put(rec.Key, req, rec.Version)}
		if previous == domain.Matched {
			rr, e := s.get(ctx, "resource/"+req.ResourceID)
			if e != nil {
				return req, e
			}
			var resource domain.Resource
			if json.Unmarshal(rr.Value, &resource) != nil || resource.Status != domain.Reserved {
				return req, ErrUnavailable
			}
			resource.Availability = req.Status == domain.Cancelled
			if resource.Availability {
				resource.Status = domain.Available
			} else {
				resource.Status = domain.Unavailable
			}
			ms = append(ms, put(rr.Key, resource, rr.Version))
			if resource.Availability {
				job, e := jobMutation("rescan")
				if e != nil {
					return req, e
				}
				ms = append(ms, job)
			}
		}
		e = s.batch(ctx, ms)
		if !errors.Is(e, storage.ErrConflict) {
			return req, e
		}
		if e = retry(ctx, attempt); e != nil {
			return req, e
		}
	}
	return domain.Request{}, storage.ErrConflict
}
func (s *Service) UpdateResource(ctx context.Context, id, status string) (domain.Resource, error) {
	if !validID(id) || (status != "available" && status != "unavailable") {
		return domain.Resource{}, domain.ErrInvalid
	}
	for attempt := 0; attempt < 16; attempt++ {
		rec, e := s.get(ctx, "resource/"+id)
		if e != nil {
			return domain.Resource{}, e
		}
		var r domain.Resource
		if json.Unmarshal(rec.Value, &r) != nil {
			return r, ErrUnavailable
		}
		if r.Status == domain.Reserved {
			return r, domain.ErrInvalid
		}
		r.Status = domain.ResourceStatus(status)
		r.Availability = status == "available"
		ms := []storage.Mutation{put(rec.Key, r, rec.Version)}
		if r.Availability {
			job, e := jobMutation("rescan")
			if e != nil {
				return r, e
			}
			ms = append(ms, job)
		}
		e = s.batch(ctx, ms)
		if !errors.Is(e, storage.ErrConflict) {
			return r, e
		}
		if e = retry(ctx, attempt); e != nil {
			return r, e
		}
	}
	return domain.Resource{}, storage.ErrConflict
}
func (s *Service) Jobs(ctx context.Context) ([]Job, error) {
	records, e := s.records(ctx)
	if e != nil {
		return nil, e
	}
	jobs := make([]Job, 0)
	for _, r := range records {
		if strings.HasPrefix(r.Key, "job/") && !r.Deleted {
			var j Job
			if json.Unmarshal(r.Value, &j) != nil || !validID(j.ID) || (!validID(j.RequestID) && j.RequestID != "rescan") || r.Key != "job/"+j.ID {
				return nil, ErrUnavailable
			}
			j.Version = r.Version
			jobs = append(jobs, j)
		}
	}
	return jobs, nil
}
func (s *Service) AckJob(ctx context.Context, j Job) error {
	e := s.batch(ctx, []storage.Mutation{{Key: "job/" + j.ID, Expected: j.Version, Delete: true}})
	if errors.Is(e, storage.ErrNotFound) || errors.Is(e, storage.ErrConflict) {
		return nil
	}
	return e
}
