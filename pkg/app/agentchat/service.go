package agentchat

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/atomicobject/rhizome/pkg/harness"
)

var (
	ErrConflict = errors.New("agent chat conflict")
	ErrNotFound = errors.New("agent session not found")
)

const statusCacheTTL = 30 * time.Second

const defaultSessionStartupTimeout = 60 * time.Second

type statusCacheEntry struct {
	status    harness.Status
	checkedAt time.Time
}

type turnState struct {
	assistant strings.Builder
}

type liveSession struct {
	session       harness.Session
	ready         chan struct{}
	drained       chan struct{}
	starting      bool
	startupCancel context.CancelFunc
	running       bool
	turn          *turnState
	pending       map[string]struct{}
	stopOnce      sync.Once
	drainOnce     sync.Once
}

type chatStore interface {
	Close() error
	CreateSession(context.Context, string, harness.Kind, string) (Session, error)
	GetSession(context.Context, string) (Session, error)
	UpdateSessionHarness(context.Context, string, harness.Kind, string, string) error
	ListSessions(context.Context, int) ([]Session, error)
	ArchiveSession(context.Context, string) error
	AddMessage(context.Context, string, string, string) (Message, error)
	Messages(context.Context, string) ([]Message, error)
	AddEvent(context.Context, Event) (Event, error)
	EventsAfter(context.Context, string, int64) ([]Event, error)
}

type Service struct {
	ctx              context.Context
	cancel           context.CancelFunc
	store            chatStore
	vaultPath        string
	harnesses        map[harness.Kind]harness.Harness
	broker           *Broker
	mu               sync.Mutex
	eventWriteMu     sync.Mutex
	live             map[string]*liveSession
	status           map[harness.Kind]statusCacheEntry
	closeOnce        sync.Once
	closeError       error
	transientEventID atomic.Int64
	startupTimeout   time.Duration
}

func NewService(ctx context.Context, vaultPath string, harnesses map[harness.Kind]harness.Harness) (*Service, error) {
	store, err := OpenStore(ctx, vaultPath)
	if err != nil {
		return nil, err
	}
	serviceCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	return &Service{
		ctx: serviceCtx, cancel: cancel, store: store, vaultPath: vaultPath,
		harnesses: harnesses, broker: NewBroker(), live: make(map[string]*liveSession),
		status:         make(map[harness.Kind]statusCacheEntry),
		startupTimeout: defaultSessionStartupTimeout,
	}, nil
}

func (s *Service) Close() error {
	if s == nil {
		return nil
	}
	s.closeOnce.Do(func() {
		s.mu.Lock()
		live := make([]*liveSession, 0, len(s.live))
		for _, item := range s.live {
			live = append(live, item)
		}
		s.live = make(map[string]*liveSession)
		s.mu.Unlock()
		for _, item := range live {
			if item.startupCancel != nil {
				item.startupCancel()
			}
		}
		var errs []error
		for _, item := range live {
			if err := stopLiveSession(item); err != nil {
				errs = append(errs, err)
			}
		}
		s.cancel()
		s.broker.Close()
		if err := s.store.Close(); err != nil {
			errs = append(errs, err)
		}
		s.closeError = errors.Join(errs...)
	})
	return s.closeError
}

func (s *Service) Settings(ctx context.Context) (SettingsResponse, error) {
	settings, err := LoadSettings()
	if err != nil {
		return SettingsResponse{}, err
	}
	statuses := s.harnessStatuses(ctx)
	return SettingsResponse{Settings: settings, Resolved: resolveHarness(settings, statuses), Harnesses: statuses}, nil
}

func (s *Service) SaveSettings(ctx context.Context, settings Settings) (SettingsResponse, error) {
	saved, err := SaveSettings(settings)
	if err != nil {
		return SettingsResponse{}, err
	}
	statuses := s.harnessStatuses(ctx)
	return SettingsResponse{Settings: saved, Resolved: resolveHarness(saved, statuses), Harnesses: statuses}, nil
}

func resolveHarness(settings Settings, statuses []HarnessStatus) ResolvedHarness {
	if settings.Harness != "" {
		return ResolvedHarness{Harness: settings.Harness, Reason: "configured"}
	}
	byKind := make(map[harness.Kind]Status, len(statuses))
	for _, item := range statuses {
		byKind[item.Kind] = item.Status
	}
	for _, kind := range harnessOrder {
		status, ok := byKind[kind]
		if ok && status.Installed && status.LoggedIn {
			return ResolvedHarness{Harness: kind, Reason: "first available"}
		}
	}
	return ResolvedHarness{Reason: "none available"}
}

func (s *Service) CreateSession(ctx context.Context, title string) (SessionResponse, error) {
	settings, err := s.Settings(ctx)
	if err != nil {
		return SessionResponse{}, err
	}
	kind := settings.Resolved.Harness
	if kind == "" {
		return SessionResponse{}, errors.New("no installed and logged-in harness is available")
	}
	model := settings.Settings.Harnesses[kind].Model
	session, err := s.store.CreateSession(ctx, title, kind, model)
	return SessionResponse{Session: session}, err
}

func (s *Service) GetSession(ctx context.Context, id string) (SessionResponse, error) {
	session, err := s.store.GetSession(ctx, id)
	if err != nil {
		return SessionResponse{}, sessionError(err)
	}
	session.TurnRunning = s.turnRunning(id)
	messages, err := s.store.Messages(ctx, id)
	if err != nil {
		return SessionResponse{}, err
	}
	events, err := s.store.EventsAfter(ctx, id, 0)
	return SessionResponse{Session: session, Messages: messages, Events: events}, err
}

func (s *Service) ListSessions(ctx context.Context) ([]Session, error) {
	sessions, err := s.store.ListSessions(ctx, 50)
	if err != nil {
		return nil, err
	}
	for i := range sessions {
		sessions[i].TurnRunning = s.turnRunning(sessions[i].ID)
	}
	return sessions, nil
}

func (s *Service) DeleteSession(ctx context.Context, sessionID string) error {
	s.mu.Lock()
	live := s.live[sessionID]
	delete(s.live, sessionID)
	s.mu.Unlock()
	if live != nil {
		if err := stopLiveSession(live); err != nil {
			return err
		}
	}
	if err := s.store.ArchiveSession(ctx, sessionID); err != nil {
		return sessionError(err)
	}
	return nil
}

func (s *Service) EventsAfter(ctx context.Context, sessionID string, afterID int64) ([]Event, error) {
	return s.store.EventsAfter(ctx, sessionID, afterID)
}

func (s *Service) Subscribe(sessionID string) (<-chan Event, func()) {
	return s.broker.Subscribe(sessionID)
}

func (s *Service) SendMessage(ctx context.Context, sessionID, content string) (SendMessageResponse, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return SendMessageResponse{}, fmt.Errorf("content is required")
	}
	session, err := s.store.GetSession(ctx, sessionID)
	if err != nil {
		return SendMessageResponse{}, sessionError(err)
	}
	if session.ReadOnly {
		return SendMessageResponse{}, fmt.Errorf("%w: pre-harness sessions are read-only", ErrConflict)
	}
	settings, err := LoadSettings()
	if err != nil {
		return SendMessageResponse{}, err
	}
	live, turn, err := s.beginTurn(session, settings)
	if err != nil {
		return SendMessageResponse{}, err
	}
	if _, err := s.store.AddMessage(s.ctx, sessionID, "user", content); err != nil {
		s.finishTurn(live, turn)
		return SendMessageResponse{}, err
	}
	go s.sendTurn(session, live, turn, content)
	updated, err := s.store.GetSession(s.ctx, sessionID)
	if err != nil {
		return SendMessageResponse{}, err
	}
	// The turn may already have completed or failed; report the live state so a
	// client cannot overwrite a terminal SSE event with a stale "running".
	updated.TurnRunning = s.turnRunning(sessionID)
	return SendMessageResponse{Session: updated}, nil
}

func (s *Service) sendTurn(session Session, live *liveSession, turn *turnState, content string) {
	if err := live.session.SendTurn(s.ctx, content); err != nil {
		if s.finishTurn(live, turn) && s.ctx.Err() == nil {
			s.persistHarnessError(session.ID, session.Harness, err)
		}
	}
}

func (s *Service) beginTurn(session Session, settings Settings) (*liveSession, *turnState, error) {
	s.mu.Lock()
	live := s.live[session.ID]
	if live != nil && (live.starting || live.running) {
		s.mu.Unlock()
		return nil, nil, fmt.Errorf("%w: a turn is already running", ErrConflict)
	}
	if live == nil {
		driver := s.harnesses[session.Harness]
		if driver == nil {
			s.mu.Unlock()
			return nil, nil, fmt.Errorf("harness %q is unavailable", session.Harness)
		}
		startupCtx, cancelStartup := context.WithTimeout(s.ctx, s.startupTimeout)
		live = &liveSession{
			ready: make(chan struct{}), drained: make(chan struct{}), starting: true,
			startupCancel: cancelStartup, pending: make(map[string]struct{}),
		}
		s.live[session.ID] = live
		s.mu.Unlock()

		configured := settings.Harnesses[session.Harness]
		harnessSession, err := driver.StartSession(startupCtx, harness.SessionOptions{
			Cwd: s.vaultPath, Model: session.Model, Effort: configured.Effort,
			PermissionMode: configured.PermissionMode, Resume: session.HarnessSessionID,
		})
		cancelStartup()
		if err != nil {
			s.failStartup(session.ID, live)
			if s.ctx.Err() == nil {
				s.persistHarnessError(session.ID, session.Harness, err)
			}
			return nil, nil, err
		}
		live.session = harnessSession
		if harnessSession.ID() != "" {
			if err := s.store.UpdateSessionHarness(s.ctx, session.ID, session.Harness, harnessSession.ID(), ""); err != nil {
				s.failStartedSession(session.ID, live)
				return nil, nil, err
			}
		}

		s.mu.Lock()
		if s.live[session.ID] != live || s.ctx.Err() != nil {
			s.mu.Unlock()
			s.failStartedSession(session.ID, live)
			return nil, nil, context.Canceled
		}
		live.starting = false
		close(live.ready)
		go s.drainEvents(session.ID, session.Harness, live)
	}
	turn := &turnState{}
	live.running = true
	live.turn = turn
	s.mu.Unlock()
	return live, turn, nil
}

func (s *Service) turnRunning(sessionID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.live[sessionID] != nil && s.live[sessionID].running
}

func sessionError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func stopLiveSession(live *liveSession) error {
	if live.startupCancel != nil {
		live.startupCancel()
	}
	<-live.ready
	var err error
	if live.session != nil {
		live.stopOnce.Do(func() { err = live.session.Stop() })
	}
	<-live.drained
	return err
}

func (s *Service) failStartup(sessionID string, live *liveSession) {
	s.mu.Lock()
	if s.live[sessionID] == live {
		delete(s.live, sessionID)
	}
	live.starting = false
	close(live.ready)
	live.drainOnce.Do(func() { close(live.drained) })
	s.mu.Unlock()
}

func (s *Service) failStartedSession(sessionID string, live *liveSession) {
	s.mu.Lock()
	if s.live[sessionID] == live {
		delete(s.live, sessionID)
	}
	live.starting = false
	close(live.ready)
	s.mu.Unlock()
	live.stopOnce.Do(func() { _ = live.session.Stop() })
	live.drainOnce.Do(func() { close(live.drained) })
}
