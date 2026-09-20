// Package app owns the single authoritative playback coordinator and show state.
package app

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"smartstage/internal/files"
	"smartstage/internal/identity"
	"smartstage/internal/model"
	"smartstage/internal/playback"
)

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string           { return e.Message }
func problem(code, message string) error { return &Error{code, message} }

type Persistence interface{ Save(model.Config) error }
type PlayRequest struct {
	RequestID  string `json:"requestId"`
	InstanceID string `json:"instanceId"`
	StopEpoch  uint64 `json:"stopEpoch"`
	CueID      string `json:"cueId"`
}
type StopRequest struct {
	RequestID string `json:"requestId"`
}
type Ack struct {
	Accepted   bool   `json:"accepted"`
	Duplicate  bool   `json:"duplicate"`
	InstanceID string `json:"instanceId"`
	Revision   uint64 `json:"revision"`
	Generation uint64 `json:"generation"`
	StopEpoch  uint64 `json:"stopEpoch"`
}
type CueView struct {
	ID         string  `json:"id"`
	Label      string  `json:"label"`
	Position   int     `json:"position"`
	Kind       string  `json:"kind"`
	Duration   float64 `json:"duration"`
	Validation string  `json:"validation"`
}
type ValidationJob struct {
	Running   bool `json:"running"`
	Completed int  `json:"completed"`
	Total     int  `json:"total"`
}
type State struct {
	InstanceID       string        `json:"instanceId"`
	Revision         uint64        `json:"revision"`
	PlaylistRevision uint64        `json:"playlistRevision"`
	State            string        `json:"state"`
	ActiveCueID      string        `json:"activeCueId"`
	ActivePosition   int           `json:"activePosition"`
	Elapsed          float64       `json:"elapsed"`
	Duration         float64       `json:"duration"`
	LastError        string        `json:"lastError"`
	Outputs          model.Outputs `json:"outputs"`
	ResolvedAudioID  string        `json:"resolvedAudioId"`
	StageEnabled     bool          `json:"stageEnabled"`
	OutputFault      bool          `json:"outputFault"`
	Generation       uint64        `json:"generation"`
	StopEpoch        uint64        `json:"stopEpoch"`
	Cues             []CueView     `json:"cues"`
	ValidationJob    ValidationJob `json:"validationJob"`
	UpdatePending    bool          `json:"updatePending"`
}
type cachedRequest struct {
	fingerprint [32]byte
	ack         Ack
}
type loadJob struct {
	ctx        context.Context
	generation uint64
	cue        model.Cue
	outputs    model.Outputs
}

type Service struct {
	mu                 sync.Mutex
	editMu             sync.Mutex // only persistence operations; never acquired by STOP
	backend            playback.Backend
	files              *files.Browser
	store              Persistence
	config             model.Config
	state              State
	ctx                context.Context
	cancel             context.CancelFunc
	loadCancel         context.CancelFunc
	loads              chan loadJob // latest-only mailbox, no cue queue
	validation         chan struct{}
	requests           map[string]cachedRequest
	requestOrder       []string
	subscribers        map[chan struct{}]struct{}
	removing           map[string]bool
	configBusy         bool
	stageEnablePending bool
	closed             bool
}

func New(backend playback.Backend, browser *files.Browser, store Persistence, config model.Config) *Service {
	ctx, cancel := context.WithCancel(context.Background())
	s := &Service{backend: backend, files: browser, store: store, config: config.Clone(), ctx: ctx, cancel: cancel,
		loads: make(chan loadJob, 1), validation: make(chan struct{}, 1), requests: map[string]cachedRequest{},
		subscribers: map[chan struct{}]struct{}{}, removing: map[string]bool{}}
	s.state = State{InstanceID: identity.New(), Revision: 1, State: "stopped", Generation: 1, StopEpoch: 1}
	// Cached results never prove readiness in a new process.
	for i := range s.config.Cues {
		s.config.Cues[i].Cache.Status = "unchecked"
	}
	go s.loadLoop()
	go s.eventLoop()
	go s.validationLoop()
	return s
}

func (s *Service) Snapshot(admin bool) State {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.state
	out.PlaylistRevision = s.config.PlaylistRevision
	out.Outputs = s.config.Outputs
	out.Cues = make([]CueView, 0, len(s.config.Cues))
	for i, c := range s.config.Cues {
		out.Cues = append(out.Cues, CueView{c.ID, c.Label, i + 1, c.Cache.Media.Kind, c.Cache.Media.Duration, c.Cache.Status})
		if c.ID == out.ActiveCueID {
			out.ActivePosition = i + 1
		}
	}
	if !admin && out.LastError != "" {
		out.LastError = "Playback or output error. Check Admin Status before retrying."
	}
	return out
}
func (s *Service) Playlist() model.Config  { s.mu.Lock(); defer s.mu.Unlock(); return s.config.Clone() }
func (s *Service) Browser() *files.Browser { return s.files }
func (s *Service) Devices(ctx context.Context) (playback.Devices, error) {
	return s.backend.Devices(ctx)
}

func (s *Service) changedLocked() {
	s.state.Revision++
	for ch := range s.subscribers {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}
func (s *Service) Subscribe() (<-chan struct{}, func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || len(s.subscribers) >= 64 {
		return nil, nil, problem("overloaded", "Too many event connections")
	}
	ch := make(chan struct{}, 1)
	ch <- struct{}{}
	s.subscribers[ch] = struct{}{}
	var once sync.Once
	return ch, func() {
		once.Do(func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			if _, ok := s.subscribers[ch]; ok {
				delete(s.subscribers, ch)
				close(ch)
			}
		})
	}, nil
}

func requestID(id string) bool {
	if len(id) < 8 || len(id) > 128 {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}
func fingerprint(v any) [32]byte { data, _ := json.Marshal(v); return sha256.Sum256(data) }
func (s *Service) duplicateLocked(id string, hash [32]byte) (Ack, bool, error) {
	if !requestID(id) {
		return Ack{}, false, problem("invalid_request", "requestId must contain 8–128 letters, digits, underscores or hyphens")
	}
	if old, ok := s.requests[id]; ok {
		if old.fingerprint != hash {
			return Ack{}, false, problem("request_conflict", "requestId was already used with different content")
		}
		ack := old.ack
		ack.Duplicate = true
		return ack, true, nil
	}
	return Ack{}, false, nil
}
func (s *Service) rememberLocked(id string, hash [32]byte) Ack {
	a := Ack{true, false, s.state.InstanceID, s.state.Revision, s.state.Generation, s.state.StopEpoch}
	if len(s.requestOrder) >= 4096 {
		delete(s.requests, s.requestOrder[0])
		s.requestOrder = s.requestOrder[1:]
	}
	s.requestOrder = append(s.requestOrder, id)
	s.requests[id] = cachedRequest{hash, a}
	return a
}
func (s *Service) invalidateLocked() {
	s.stageEnablePending = false
	if s.loadCancel != nil {
		s.loadCancel()
		s.loadCancel = nil
	}
	select {
	case <-s.loads:
	default:
	}
	s.state.Generation++
}
func (s *Service) clearActiveLocked() {
	s.state.ActiveCueID = ""
	s.state.ActivePosition = 0
	s.state.Elapsed = 0
	s.state.Duration = 0
	s.state.ResolvedAudioID = ""
}

func (s *Service) Play(r PlayRequest) (Ack, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	hash := fingerprint(r)
	if a, ok, err := s.duplicateLocked(r.RequestID, hash); ok || err != nil {
		return a, err
	}
	if s.closed {
		return Ack{}, problem("unavailable", "Application is shutting down")
	}
	if s.state.UpdatePending {
		return Ack{}, problem("updating", "An application update is being prepared; playback is temporarily unavailable")
	}
	if r.InstanceID != s.state.InstanceID {
		return Ack{}, problem("stale_instance", "Refresh state from this application instance")
	}
	if r.StopEpoch != s.state.StopEpoch {
		return Ack{}, problem("stale_epoch", "STOP invalidated this PLAY; refresh state before an intentional new cue")
	}
	if s.configBusy || s.removing[r.CueID] {
		return Ack{}, problem("busy", "Configuration is being saved; try again")
	}
	if s.state.OutputFault {
		return Ack{}, problem("output_unavailable", "Select and save available outputs in Admin before retrying")
	}
	var cue *model.Cue
	for i := range s.config.Cues {
		if s.config.Cues[i].ID == r.CueID {
			c := s.config.Cues[i]
			cue = &c
			break
		}
	}
	if cue == nil {
		return Ack{}, problem("cue_not_found", "Cue does not exist")
	}
	if cue.Cache.Status == "missing" || cue.Cache.Status == "unsupported" || cue.Cache.Status == "error" {
		return Ack{}, problem("cue_invalid", "Cue needs successful validation in Admin before playback")
	}
	s.invalidateLocked()
	s.state.State = "loading"
	s.state.ActiveCueID = cue.ID
	s.state.LastError = ""
	s.state.Elapsed = 0
	s.state.Duration = cue.Cache.Media.Duration
	s.state.ResolvedAudioID = ""
	if err := s.backend.Stop(s.state.Generation); err != nil {
		s.failLocked(err.Error())
		return s.rememberLocked(r.RequestID, hash), nil
	}
	ctx, cancel := context.WithCancel(s.ctx)
	s.loadCancel = cancel
	s.loads <- loadJob{ctx, s.state.Generation, *cue, s.config.Outputs}
	s.changedLocked()
	return s.rememberLocked(r.RequestID, hash), nil
}
func (s *Service) Stop(r StopRequest) (Ack, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	hash := fingerprint(struct {
		Kind    string
		Request StopRequest
	}{"stop", r})
	if a, ok, err := s.duplicateLocked(r.RequestID, hash); ok || err != nil {
		return a, err
	}
	s.stopLocked()
	return s.rememberLocked(r.RequestID, hash), nil
}
func (s *Service) stopLocked() {
	s.invalidateLocked()
	s.state.StopEpoch++
	s.clearActiveLocked()
	s.state.State = "stopping"
	s.state.LastError = ""
	if err := s.backend.Stop(s.state.Generation); err != nil {
		s.state.State = "error"
		s.state.LastError = err.Error()
	}
	s.changedLocked()
}
func (s *Service) failLocked(message string) {
	s.invalidateLocked()
	s.state.StopEpoch++
	s.clearActiveLocked()
	s.state.State = "error"
	s.state.LastError = message
	_ = s.backend.Stop(s.state.Generation)
	s.changedLocked()
}
func (s *Service) loadLoop() {
	for {
		select {
		case <-s.ctx.Done():
			return
		case job := <-s.loads:
			s.prepare(job)
		}
	}
}
func (s *Service) inspect(ctx context.Context, path string) model.Validation {
	resolved, info, err := s.files.File(path)
	if err != nil {
		status := "error"
		if errors.Is(err, os.ErrNotExist) {
			status = "missing"
		}
		return model.Validation{Status: status, Reason: err.Error()}
	}
	m, err := s.backend.Inspect(ctx, resolved)
	v := model.Validation{Status: "ready", Media: m, Size: info.Size(), Modified: info.ModTime().UnixNano()}
	if err != nil {
		v.Status = "unsupported"
		v.Reason = err.Error()
	}
	return v
}
func (s *Service) InspectFile(ctx context.Context, path string) model.Validation {
	ctx, cancel := context.WithTimeout(ctx, 35*time.Second)
	defer cancel()
	return s.inspect(ctx, path)
}

func (s *Service) prepare(job loadJob) {
	// Filesystem access and decoder preparation run here, never on the command
	// mutex, HTTP STOP path, or native UI event loop.
	path, info, err := s.files.File(job.cue.Path)
	cache := job.cue.Cache
	if err != nil {
		cache.Status = "error"
		if errors.Is(err, os.ErrNotExist) {
			cache.Status = "missing"
		}
		cache.Reason = err.Error()
	}
	if err == nil && (cache.Status != "ready" || info.Size() != cache.Size || info.ModTime().UnixNano() != cache.Modified) {
		cache = s.inspect(job.ctx, path)
		if cache.Status != "ready" {
			err = errors.New(cache.Reason)
		}
	}
	var devices playback.Devices
	if err == nil {
		devices, err = s.backend.Devices(job.ctx)
	}
	resolvedAudio := ""
	if err == nil && cache.Media.HasAudio {
		for _, d := range devices.Audio {
			if d.ID == job.outputs.AudioID || job.outputs.AudioID == "default" && d.Default {
				resolvedAudio = d.ID
				break
			}
		}
		if resolvedAudio == "" {
			err = errors.New("Selected audio output is unavailable; select an output in Admin")
		}
	}
	if err == nil && cache.Media.HasVideo {
		err = checkDisplay(devices, job.outputs)
	}
	if job.ctx.Err() != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.Generation != job.generation || s.state.State != "loading" || s.closed {
		return
	}
	for i := range s.config.Cues {
		if s.config.Cues[i].ID == job.cue.ID && s.config.Cues[i].Path == job.cue.Path {
			s.config.Cues[i].Cache = cache
		}
	}
	if err != nil {
		s.failLocked(err.Error())
		return
	}
	s.state.ResolvedAudioID = resolvedAudio
	s.state.Duration = cache.Media.Duration
	if err = s.backend.Start(playback.Start{Generation: job.generation, Path: path, AudioID: resolvedAudio, DisplayID: job.outputs.DisplayID, Video: cache.Media.HasVideo}); err != nil {
		s.failLocked(err.Error())
		return
	}
	s.changedLocked()
}
func checkDisplay(devices playback.Devices, outputs model.Outputs) error {
	for _, d := range devices.Displays {
		if d.ID == outputs.DisplayID {
			if (d.Primary || len(devices.Displays) == 1) && !outputs.AllowPrimary {
				return problem("primary_confirmation", "Confirm use of the primary/only display in Admin Outputs")
			}
			return nil
		}
	}
	return problem("display_unavailable", "Select an available stage display in Admin")
}
func (s *Service) eventLoop() {
	for {
		select {
		case <-s.ctx.Done():
			return
		case e, ok := <-s.backend.Events():
			if !ok {
				s.mu.Lock()
				if !s.closed {
					s.failLocked("Native backend event stream closed")
				}
				s.mu.Unlock()
				return
			}
			s.nativeEvent(e)
		}
	}
}
func (s *Service) nativeEvent(e playback.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	if e.Kind == "devices" {
		s.changedLocked()
		return
	}
	// A local emergency STOP, like an HTTP STOP, has no stale-generation
	// precondition. It must still stop a concurrently accepted controller PLAY.
	if e.Kind == "escape" {
		s.stopLocked()
		return
	}
	if e.Generation != s.state.Generation {
		return
	}
	s.state.StageEnabled = e.StageEnabled
	if e.StageEnabled {
		s.stageEnablePending = false
	}
	switch e.Kind {
	case "playing":
		if s.state.State != "loading" {
			return
		}
		s.state.State = "playing"
		s.state.Duration = e.Duration
	case "progress":
		if s.state.State != "playing" {
			return
		}
		s.state.Elapsed = e.Position
		if e.Duration > 0 {
			s.state.Duration = e.Duration
		}
	case "ended":
		if s.state.State != "playing" && s.state.State != "loading" {
			return
		}
		s.clearActiveLocked()
		s.state.State = "stopped"
	case "stopped":
		if s.state.State != "stopping" && s.state.State != "stopped" {
			return
		}
		s.state.State = "stopped"
	case "error", "device-lost":
		if e.Kind == "device-lost" {
			s.state.OutputFault = true
		}
		s.failLocked(e.Message)
		return
	default:
		return
	}
	s.changedLocked()
}
func (s *Service) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.stopLocked()
	s.closed = true
	s.cancel()
	for ch := range s.subscribers {
		close(ch)
		delete(s.subscribers, ch)
	}
}

func ValidateLabel(label string) error {
	if strings.TrimSpace(label) == "" || len(label) > 512 || strings.ContainsAny(label, "\x00\r\n") {
		return problem("invalid_label", "Labels must contain 1–512 UTF-8 bytes without line breaks")
	}
	return nil
}
func (s *Service) String() string {
	return fmt.Sprintf("Smart Stage instance %s", s.Snapshot(false).InstanceID)
}
