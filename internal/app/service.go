// Package app owns the single authoritative playback coordinator and show state.
package app

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math"
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
	RequestID         string   `json:"requestId"`
	InstanceID        string   `json:"instanceId"`
	StopEpoch         uint64   `json:"stopEpoch"`
	CueID             string   `json:"cueId"`
	Action            string   `json:"action,omitempty"`
	Generation        uint64   `json:"generation,omitempty"`
	TransportRevision uint64   `json:"transportRevision,omitempty"`
	Position          *float64 `json:"position,omitempty"`
}
type StopRequest struct {
	RequestID string `json:"requestId"`
}
type Ack struct {
	Accepted          bool   `json:"accepted"`
	Duplicate         bool   `json:"duplicate"`
	InstanceID        string `json:"instanceId"`
	Revision          uint64 `json:"revision"`
	Generation        uint64 `json:"generation"`
	StopEpoch         uint64 `json:"stopEpoch"`
	TransportRevision uint64 `json:"transportRevision"`
}
type CueView struct {
	ID         string  `json:"id"`
	Label      string  `json:"label"`
	Color      string  `json:"color,omitempty"`
	Hidden     bool    `json:"hidden,omitempty"`
	Background bool    `json:"background,omitempty"`
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
	InstanceID        string              `json:"instanceId"`
	Revision          uint64              `json:"revision"`
	PlaylistRevision  uint64              `json:"playlistRevision"`
	State             string              `json:"state"`
	ActiveCueID       string              `json:"activeCueId"`
	ActivePosition    int                 `json:"activePosition"`
	Elapsed           float64             `json:"elapsed"`
	Duration          float64             `json:"duration"`
	LastError         string              `json:"lastError"`
	Outputs           model.Outputs       `json:"outputs"`
	Stage             model.StageSettings `json:"stage"`
	BackgroundCueID   string              `json:"backgroundCueId"`
	ImageCueID        string              `json:"imageCueId"`
	BackgroundError   string              `json:"backgroundError"`
	ResolvedAudioID   string              `json:"resolvedAudioId"`
	StageEnabled      bool                `json:"stageEnabled"`
	OutputFault       bool                `json:"outputFault"`
	Generation        uint64              `json:"generation"`
	StopEpoch         uint64              `json:"stopEpoch"`
	TransportRevision uint64              `json:"transportRevision"`
	Paused            bool                `json:"paused"`
	SeekPending       bool                `json:"seekPending"`
	Cues              []CueView           `json:"cues"`
	ValidationJob     ValidationJob       `json:"validationJob"`
	UpdatePending     bool                `json:"updatePending"`
}
type cachedRequest struct {
	fingerprint [32]byte
	ack         Ack
}
type loadJob struct {
	ctx         context.Context
	generation  uint64
	cue         model.Cue
	outputs     model.Outputs
	stageSerial uint64
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
	sceneRevision      uint64
	stageSerial        uint64
	stageDesired       bool
	foreground         presentationSource
	image              presentationSource
	background         presentationSource
	visualLoads        chan visualJob
	backgroundLoads    chan visualJob
	visualSequence     uint64
	backgroundSequence uint64
	visualCancel       context.CancelFunc
	backgroundCancel   context.CancelFunc
	pendingImageID     string
	legacyForegroundID uint64
	legacyStage        bool
}

func New(backend playback.Backend, browser *files.Browser, store Persistence, config model.Config) *Service {
	ctx, cancel := context.WithCancel(context.Background())
	if config.Stage.AudioFadeSeconds == 0 {
		config.Stage.AudioFadeSeconds = 1
	}
	if config.Stage.VisualFadeSeconds == 0 {
		config.Stage.VisualFadeSeconds = 1
	}
	s := &Service{backend: backend, files: browser, store: store, config: config.Clone(), ctx: ctx, cancel: cancel,
		loads: make(chan loadJob, 1), validation: make(chan struct{}, 1), requests: map[string]cachedRequest{},
		subscribers: map[chan struct{}]struct{}{}, removing: map[string]bool{}, visualLoads: make(chan visualJob, 1), backgroundLoads: make(chan visualJob, 1)}
	s.state = State{InstanceID: identity.New(), Revision: 1, State: "stopped", Generation: 1, StopEpoch: 1}
	// Cached results never prove readiness in a new process.
	for i := range s.config.Cues {
		s.config.Cues[i].Cache.Status = "unchecked"
	}
	go s.loadLoop()
	go s.eventLoop()
	go s.validationLoop()
	go s.visualLoop(s.visualLoads, false)
	go s.visualLoop(s.backgroundLoads, true)
	s.mu.Lock()
	s.selectBackgroundLocked(config.Stage.BackgroundCueID)
	s.mu.Unlock()
	return s
}

func (s *Service) Snapshot(admin bool) State {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.state
	out.PlaylistRevision = s.config.PlaylistRevision
	out.Outputs = s.config.Outputs
	out.Stage = s.config.Stage
	out.Cues = make([]CueView, 0, len(s.config.Cues))
	for i, c := range s.config.Cues {
		out.Cues = append(out.Cues, CueView{ID: c.ID, Label: c.Label, Color: c.Color, Hidden: c.Hidden, Background: c.Background, Position: i + 1, Kind: c.Cache.Media.Kind, Duration: c.Cache.Media.Duration, Validation: c.Cache.Status})
		if c.ID == out.ActiveCueID {
			out.ActivePosition = i + 1
		}
	}
	if !admin && out.LastError != "" {
		out.LastError = "Playback or output error. Check Admin Status before retrying."
	}
	if !admin && out.BackgroundError != "" {
		out.BackgroundError = "Background unavailable. Check Admin."
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
	a := Ack{Accepted: true, InstanceID: s.state.InstanceID, Revision: s.state.Revision, Generation: s.state.Generation, StopEpoch: s.state.StopEpoch, TransportRevision: s.state.TransportRevision}
	if len(s.requestOrder) >= 4096 {
		delete(s.requests, s.requestOrder[0])
		s.requestOrder = s.requestOrder[1:]
	}
	s.requestOrder = append(s.requestOrder, id)
	s.requests[id] = cachedRequest{hash, a}
	return a
}
func (s *Service) invalidateLocked() {
	if s.loadCancel != nil {
		s.loadCancel()
		s.loadCancel = nil
	}
	select {
	case <-s.loads:
	default:
	}
	s.state.Generation++
	s.state.TransportRevision++
}
func (s *Service) clearActiveLocked() {
	s.state.ActiveCueID = ""
	s.state.ActivePosition = 0
	s.state.Elapsed = 0
	s.state.Duration = 0
	s.state.ResolvedAudioID = ""
	s.state.Paused = false
	s.state.SeekPending = false
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
	if r.Action != "" && r.Action != "play" {
		if err := s.transportLocked(r); err != nil {
			return Ack{}, err
		}
		return s.rememberLocked(r.RequestID, hash), nil
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
	// Selected image buttons retain their toggle behavior. Foreground buttons
	// pause/resume, including intent accepted while preparation is still pending.
	if !cue.Background {
		if cue.ID == s.pendingImageID || cue.ID == s.state.ImageCueID && s.stageDesired {
			s.stopImageLocked()
			return s.rememberLocked(r.RequestID, hash), nil
		}
		if cue.ID == s.state.ActiveCueID {
			if err := s.pauseLocked(!s.state.Paused); err != nil {
				return Ack{}, err
			}
			return s.rememberLocked(r.RequestID, hash), nil
		}
	}
	if cue.Cache.Status == "missing" || cue.Cache.Status == "unsupported" || cue.Cache.Status == "error" {
		return Ack{}, problem("cue_invalid", "Cue needs successful validation in Admin before playback")
	}
	if cue.Background {
		s.selectBackgroundLocked(cue.ID)
		s.changedLocked()
		return s.rememberLocked(r.RequestID, hash), nil
	}
	if cue.Cache.Media.Kind == "image" || cue.Cache.Media.Kind == "" && imageFile(cue.Path) {
		s.selectImageLocked(*cue)
		s.changedLocked()
		return s.rememberLocked(r.RequestID, hash), nil
	}
	s.clearImageLocked()
	s.invalidateLocked()
	s.state.State = "loading"
	s.state.ActiveCueID = cue.ID
	s.state.LastError = ""
	s.state.Elapsed = 0
	s.state.Duration = cue.Cache.Media.Duration
	s.state.ResolvedAudioID = ""
	s.state.Paused = false
	s.state.SeekPending = false
	// Keep the previous native source audible while the replacement is checked
	// and prepared. The native scene mixer performs the eventual crossfade.
	ctx, cancel := context.WithCancel(s.ctx)
	s.loadCancel = cancel
	s.loads <- loadJob{ctx: ctx, generation: s.state.Generation, cue: *cue, outputs: s.config.Outputs, stageSerial: s.stageSerial}
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
	s.stopForegroundLocked(true, false)
}

func (s *Service) stopForegroundLocked(clearImage, hard bool) {
	s.invalidateLocked()
	s.state.StopEpoch++
	s.clearActiveLocked()
	s.foreground = presentationSource{}
	if clearImage {
		s.clearImageLocked()
	}
	s.state.State = "stopping"
	s.state.LastError = ""
	if err := s.applySceneLocked(hard); err != nil {
		s.state.State = "error"
		s.state.LastError = err.Error()
	}
	s.changedLocked()
}
func (s *Service) failLocked(message string) {
	s.invalidateLocked()
	s.state.StopEpoch++
	s.clearActiveLocked()
	s.foreground = presentationSource{}
	s.clearImageLocked()
	s.stageDesired = false
	s.stageSerial++
	s.stageEnablePending = false
	s.state.StageEnabled = false
	s.state.State = "error"
	s.state.LastError = message
	_ = s.applySceneLocked(true)
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
	if err == nil && (cache.Media.HasVideo || cache.Media.Kind == "image") {
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
	// An image added before validation still preserves any existing music.
	// Usually images are dispatched directly by Play after playlist validation.
	if cache.Media.Kind == "image" {
		s.state.ActiveCueID = s.foreground.cueID
		if s.foreground.id == 0 {
			s.state.State = "stopped"
		} else if s.foreground.paused {
			s.state.State = "paused"
		} else {
			s.state.State = "playing"
		}
		s.state.Paused = s.foreground.paused
		s.state.SeekPending = s.foreground.seekPending
		s.state.Elapsed = s.foreground.position
		s.state.Duration = s.foreground.duration
		s.state.ResolvedAudioID = s.foreground.audioID
		// Inspection corrected an unknown/stale kind to image. Restore the
		// independent source without restarting it, and acknowledge its current
		// transport under the latest controller revision.
		s.foreground.transportRevision = s.state.TransportRevision
		s.image = presentationSource{cueID: job.cue.ID, path: path, kind: "image"}
		s.state.ImageCueID = job.cue.ID
		if job.stageSerial == s.stageSerial {
			s.stageDesired = true
		}
		if err = s.applySceneLocked(false); err != nil {
			s.failLocked(err.Error())
			return
		}
		s.changedLocked()
		return
	}
	s.state.ResolvedAudioID = resolvedAudio
	s.state.Duration = cache.Media.Duration
	s.foreground = presentationSource{id: job.generation, cueID: job.cue.ID, path: path, kind: cache.Media.Kind, hasAudio: cache.Media.HasAudio, audioID: resolvedAudio, duration: cache.Media.Duration, paused: s.state.Paused, transportRevision: s.state.TransportRevision}
	if cache.Media.HasVideo && job.stageSerial == s.stageSerial {
		s.stageDesired = true
	}
	if err = s.applySceneLocked(false); err != nil {
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
		s.stageDesired = false
		s.stageSerial++
		s.stopForegroundLocked(true, true)
		// Escape closes the stage as well as stopping. Apply the decision to
		// the current generation even when a concurrent PLAY made the native
		// key event's generation stale.
		s.state.StageEnabled = false
		s.changedLocked()
		return
	}
	if e.Kind == "stage" || e.Kind == "background-error" {
		if e.SceneRevision != s.sceneRevision {
			return
		}
		s.state.StageEnabled = e.StageEnabled
		s.stageEnablePending = false
		if e.Kind == "background-error" {
			s.state.BackgroundError = e.Message
		}
		s.changedLocked()
		return
	}
	// Output loss belongs to the currently committed native scene, including
	// while a replacement cue is still being inspected. Cancel that replacement
	// instead of letting it rearm a scene after the native emergency shutdown.
	if e.Kind == "device-lost" && e.SceneRevision != 0 && e.SceneRevision == s.sceneRevision {
		s.state.OutputFault = true
		s.failLocked(e.Message)
		return
	}
	// Global failures of the committed scene (for example a stage operation
	// during replacement inspection) belong to its command generation rather
	// than the previous source's independent transport revision.
	if e.Kind == "error" && e.SceneRevision != 0 && e.SceneRevision == s.sceneRevision && e.Generation == s.state.Generation && s.foreground.id != s.state.Generation {
		s.failLocked(e.Message)
		return
	}
	expected := s.state.Generation
	if (s.state.State == "playing" || s.state.State == "paused") && s.foreground.id != 0 {
		expected = s.foreground.id
	}
	if e.Generation != expected {
		return
	}
	if _, native := s.backend.(playback.SceneBackend); native && (e.Kind == "playing" || e.Kind == "paused" || e.Kind == "progress" || e.Kind == "ended" || e.Kind == "error") && e.TransportRevision != s.state.TransportRevision {
		return
	}
	// Music keeps its identity across visual changes. Its queued progress may
	// describe the previous stage visibility, but its timeline is still valid.
	if e.SceneRevision == s.sceneRevision || e.SceneRevision == 0 {
		s.state.StageEnabled = e.StageEnabled
		if e.StageEnabled {
			s.stageEnablePending = false
		}
	}
	switch e.Kind {
	case "playing", "paused":
		if s.state.State != "loading" && s.state.State != "playing" && s.state.State != "paused" || (e.Kind == "paused") != s.state.Paused {
			return
		}
		s.state.State = e.Kind
		s.state.SeekPending = false
		s.foreground.seekPending = false
		if finitePosition(e.Position) {
			s.state.Elapsed = e.Position
			s.foreground.position = e.Position
		}
		if e.Duration > 0 && finitePosition(e.Duration) {
			s.state.Duration = e.Duration
			s.foreground.duration = e.Duration
		}
	case "progress":
		if s.state.State != "playing" && s.state.State != "paused" {
			return
		}
		if finitePosition(e.Position) {
			s.state.Elapsed = e.Position
			s.foreground.position = e.Position
		}
		if e.Duration > 0 && finitePosition(e.Duration) {
			s.state.Duration = e.Duration
			s.foreground.duration = e.Duration
		}
	case "ended":
		// An exact-revision terminal event also acknowledges a source that
		// naturally ended before a newly accepted pause reached native. Native
		// backends keep paused seeks to the duration selected until resumed.
		if s.state.State != "playing" && s.state.State != "loading" && s.state.State != "paused" {
			return
		}
		s.clearActiveLocked()
		s.foreground = presentationSource{}
		s.state.State = "stopped"
		if err := s.applySceneLocked(false); err != nil {
			s.failLocked(err.Error())
			return
		}
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

func finitePosition(value float64) bool {
	return value >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}
func (s *Service) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.stageDesired = false
	s.stageSerial++
	s.stopForegroundLocked(true, true)
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
