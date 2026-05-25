package iptv

import (
	"sync"
	"time"

	"hubplay/internal/clock"
)

// breakerState — máquina de tres estados del circuit breaker per-canal.
type breakerState int

const (
	breakerClosed breakerState = iota
	breakerOpen
	breakerHalfOpen
)

func (s breakerState) String() string {
	switch s {
	case breakerOpen:
		return "open"
	case breakerHalfOpen:
		return "half-open"
	default:
		return "closed"
	}
}

// breakerEntry — estado runtime de un canal. Solo se muta bajo mu.
type breakerEntry struct {
	state         breakerState
	failures      int       // consecutive upstream failures while closed
	openedAt      time.Time // when it transitioned to open
	cooldown      time.Duration
	lastChange    time.Time // for pruning idle entries
	trialInFlight bool      // at most one half-open probe at a time
}

const (
	breakerThreshold       = 5
	breakerInitialCooldown = 30 * time.Second
	breakerMaxCooldown     = 5 * time.Minute
	// Si un trial half-open no termina en este plazo, el slot se
	// libera. Protege contra el caso donde el ctx se cancela
	// mid-fetch y la rama de cancel no llama a Record*.
	breakerTrialTimeout = 30 * time.Second
	// Entradas cerradas sin fallos recientes se eviccionan tras este
	// periodo para no crecer el map indefinidamente.
	breakerIdleEvictAfter = 10 * time.Minute
)

// channelBreaker — fast-fail per-canal delante del upstream del proxy.
// Sin DB, sin HTTP. Indexado por channelID porque per-URL explota con
// segmentos y per-host es demasiado amplio (un token caducado en CDN
// compartido penalizaría todos los canales allí).
type channelBreaker struct {
	mu      sync.Mutex
	entries map[string]*breakerEntry
	clk     clock.Clock
}

func newChannelBreaker(clk clock.Clock) *channelBreaker {
	if clk == nil {
		clk = clock.New()
	}
	return &channelBreaker{
		entries: make(map[string]*breakerEntry),
		clk:     clk,
	}
}

// Allow indica si se permite un intento upstream ahora. Efecto
// secundario: al expirar el cooldown, promueve a half-open y reserva
// el slot de trial para este caller (evita thundering herd).
func (b *channelBreaker) Allow(channelID string) (bool, time.Duration) {
	if channelID == "" {
		// Sin channelID: skip para no crear entrada fantasma en "".
		return true, 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	e, ok := b.entries[channelID]
	if !ok {
		return true, 0
	}

	now := b.clk.Now()
	switch e.state {
	case breakerClosed:
		return true, 0
	case breakerOpen:
		elapsed := now.Sub(e.openedAt)
		if elapsed < e.cooldown {
			return false, e.cooldown - elapsed
		}
		// Cooldown expirado — promover a half-open.
		e.state = breakerHalfOpen
		e.trialInFlight = true
		e.lastChange = now
		return true, 0
	case breakerHalfOpen:
		if e.trialInFlight && now.Sub(e.lastChange) < breakerTrialTimeout {
			return false, breakerTrialTimeout - now.Sub(e.lastChange)
		}
		// Sin trial en vuelo o trial previo expirado.
		e.trialInFlight = true
		e.lastChange = now
		return true, 0
	}
	return true, 0
}

// RecordSuccess cierra el breaker y resetea el contador de fallos.
func (b *channelBreaker) RecordSuccess(channelID string) {
	if channelID == "" {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	e, ok := b.entries[channelID]
	if !ok {
		// Sin fallos previos — nada que registrar.
		return
	}
	e.state = breakerClosed
	e.failures = 0
	e.cooldown = 0
	e.trialInFlight = false
	e.lastChange = b.clk.Now()
}

// RecordFailure incrementa fallos (closed) o reabre (half-open).
// Cooldown inicial en closed→open; se duplica en half-open→open
// hasta breakerMaxCooldown.
func (b *channelBreaker) RecordFailure(channelID string) {
	if channelID == "" {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.clk.Now()
	e, ok := b.entries[channelID]
	if !ok {
		e = &breakerEntry{state: breakerClosed, lastChange: now}
		b.entries[channelID] = e
	}
	switch e.state {
	case breakerClosed:
		e.failures++
		if e.failures >= breakerThreshold {
			e.state = breakerOpen
			e.openedAt = now
			e.cooldown = breakerInitialCooldown
			e.lastChange = now
		}
	case breakerHalfOpen:
		// Trial fallido — volver a open con cooldown más largo.
		e.state = breakerOpen
		e.openedAt = now
		next := e.cooldown * 2
		if next < breakerInitialCooldown {
			next = breakerInitialCooldown
		}
		if next > breakerMaxCooldown {
			next = breakerMaxCooldown
		}
		e.cooldown = next
		e.trialInFlight = false
		e.lastChange = now
	case breakerOpen:
		// Ya abierto. Raro llegar aquí (Allow lo previene).
		// Refrescar lastChange para evitar evicción por idle.
		e.lastChange = now
	}
}

// State devuelve etiqueta humana y cooldown restante para el dashboard admin.
func (b *channelBreaker) State(channelID string) (string, time.Duration) {
	if channelID == "" {
		return "closed", 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	e, ok := b.entries[channelID]
	if !ok {
		return "closed", 0
	}
	switch e.state {
	case breakerOpen:
		now := b.clk.Now()
		remaining := e.cooldown - now.Sub(e.openedAt)
		if remaining < 0 {
			remaining = 0
		}
		return "open", remaining
	case breakerHalfOpen:
		return "half-open", 0
	default:
		return "closed", 0
	}
}

// Prune elimina entradas cerradas sin fallos recientes. Idempotente.
func (b *channelBreaker) Prune() {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.clk.Now()
	for k, e := range b.entries {
		if e.state == breakerClosed && e.failures == 0 &&
			now.Sub(e.lastChange) > breakerIdleEvictAfter {
			delete(b.entries, k)
		}
	}
}
