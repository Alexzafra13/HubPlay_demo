package handlers

import (
	"errors"
	"sync"
	"time"
)

// Defaults dimensionados para un servidor self-hosted: un hogar de ~10
// usuarios con 2-3 pestañas cada uno cabe bajo 100 global, y un solo
// usuario abriendo más de 5 /me/events concurrentes es casi con seguridad
// un bucle de reconexión descontrolado y no uso legítimo.
const (
	DefaultSSEGlobalMax  = 100
	DefaultSSEPerUserMax = 5
)

// ErrSSEGlobalCap y ErrSSEPerUserCap permiten a los callers distinguir "el
// servidor está saturado" de "este usuario nos está martilleando"; hoy
// ambos se exponen como el mismo 503 al cliente, pero la distinción importa
// para logs / futura telemetría por-usuario.
var (
	ErrSSEGlobalCap  = errors.New("sse: global connection cap reached")
	ErrSSEPerUserCap = errors.New("sse: per-user connection cap reached")
)

// SSELimiter acota las conexiones concurrentes de Server-Sent Events. Una
// instancia es compartida por cada handler SSE (events, me_events,
// admin_logs) para que el cap global sea de verdad global — contado a
// través de superficies, no por-handler.
//
// Cada conexión SSE además suscribe 1-20 callbacks al event bus y retiene
// una goroutine + canal con buffer; sin un cap, un cliente malicioso o con
// bug puede abrir miles y agotar tanto la memoria como la latencia de
// dispatch del bus.
type SSELimiter struct {
	globalMax  int
	perUserMax int

	mu      sync.Mutex
	global  int
	perUser map[string]int

	// changes señaliza cada Acquire/release para que los tests puedan
	// esperar transiciones sin polling con time.Sleep. Buffer 32 +
	// envío non-blocking: producción nunca lee.
	changes chan struct{}
}

func NewSSELimiter(globalMax, perUserMax int) *SSELimiter {
	if globalMax <= 0 {
		globalMax = DefaultSSEGlobalMax
	}
	if perUserMax <= 0 {
		perUserMax = DefaultSSEPerUserMax
	}
	return &SSELimiter{
		globalMax:  globalMax,
		perUserMax: perUserMax,
		perUser:    make(map[string]int),
		changes:    make(chan struct{}, 32),
	}
}

// Acquire reserva un slot de conexión para userID. La función release
// devuelta es idempotente — los callers pueden hacerle defer sin
// preocuparse de un doble decremento. Un userID vacío cuenta sólo hacia el
// cap global (sin tracking por-usuario); los callers actuales siempre
// aportan claims.UserID, pero la excepción mantiene la API usable para
// cualquier futura superficie SSE no autenticada.
func (l *SSELimiter) Acquire(userID string) (release func(), err error) {
	l.mu.Lock()
	if l.global >= l.globalMax {
		l.mu.Unlock()
		return nil, ErrSSEGlobalCap
	}
	if userID != "" && l.perUser[userID] >= l.perUserMax {
		l.mu.Unlock()
		return nil, ErrSSEPerUserCap
	}
	l.global++
	if userID != "" {
		l.perUser[userID]++
	}
	l.mu.Unlock()
	l.notifyChange()
	var once sync.Once
	return func() {
		once.Do(func() {
			l.mu.Lock()
			l.global--
			if userID != "" {
				l.perUser[userID]--
				if l.perUser[userID] <= 0 {
					delete(l.perUser, userID)
				}
			}
			l.mu.Unlock()
			l.notifyChange()
		})
	}, nil
}

func (l *SSELimiter) notifyChange() {
	select {
	case l.changes <- struct{}{}:
	default:
	}
}

// Snapshot devuelve el contador global actual y una copia del mapa
// por-usuario. Pensado para tests y futura observability — no está en
// ningún hot path, así que el coste de copiar el mapa es aceptable.
func (l *SSELimiter) Snapshot() (global int, perUser map[string]int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make(map[string]int, len(l.perUser))
	for k, v := range l.perUser {
		out[k] = v
	}
	return l.global, out
}

// WaitForGlobal bloquea hasta que el contador global == want o el
// timeout vence. Devuelve true si la condición se cumplió. Test-only.
func (l *SSELimiter) WaitForGlobal(want int, timeout time.Duration) bool {
	deadline := time.After(timeout)
	for {
		l.mu.Lock()
		got := l.global
		l.mu.Unlock()
		if got == want {
			return true
		}
		select {
		case <-l.changes:
		case <-deadline:
			l.mu.Lock()
			got := l.global
			l.mu.Unlock()
			return got == want
		}
	}
}
