package upload

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"time"

	"github.com/tus/tusd/v2/pkg/filestore"
	tushandler "github.com/tus/tusd/v2/pkg/handler"

	"hubplay/internal/auth"
)

// TusdHandler envuelve un tusd handler con nuestros hooks. Cabletea:
//   - PreUploadCreateCallback → Service.PreCreate
//   - canal CompleteUploads → Service.Finish en goroutine
//   - canal TerminatedUploads → Service.Aborted
//
// El handler resultante es un http.Handler — el router chi lo monta
// bajo /api/uploads/* con la auth middleware delante para que las
// claims estén en el contexto cuando los hooks corren.
type TusdHandler struct {
	svc      *Service
	handler  *tushandler.Handler
	basePath string
}

// NewTusdHandler crea el handler tusd con la FileStore apuntando al
// staging dir del Service. basePath es el prefijo URL bajo el que se
// monta (debe terminar en "/"; tusd añade el id por su cuenta).
func NewTusdHandler(svc *Service, basePath string) (*TusdHandler, error) {
	if svc == nil {
		return nil, fmt.Errorf("nil service")
	}
	if basePath == "" {
		return nil, fmt.Errorf("basePath required")
	}

	store := filestore.New(svc.staging.Root())
	composer := tushandler.NewStoreComposer()
	store.UseIn(composer)

	th := &TusdHandler{svc: svc, basePath: basePath}

	cfg := tushandler.Config{
		BasePath:                   basePath,
		StoreComposer:              composer,
		NotifyCompleteUploads:      true,
		NotifyTerminatedUploads:    true,
		PreUploadCreateCallback:    th.preCreate,
		PreUploadTerminateCallback: th.preTerminate,
		// tusd v2.9 sigue usando golang.org/x/exp/slog en su firma,
		// distinto del stdlib log/slog del proyecto. No le pasamos
		// logger — tusd cae a su default y nuestros hooks loguean
		// vía svc.logger igualmente.
	}

	h, err := tushandler.NewHandler(cfg)
	if err != nil {
		return nil, fmt.Errorf("tusd NewHandler: %w", err)
	}
	th.handler = h

	go th.consumeCompletes()
	go th.consumeTerminations()

	return th, nil
}

// ServeHTTP delega en el handler tusd. Las rutas que tusd entiende
// dentro de basePath son:
//   POST   {basePath}            → crear upload
//   PATCH  {basePath}{id}        → enviar chunk
//   HEAD   {basePath}{id}        → estado
//   DELETE {basePath}{id}        → cancelar
//   OPTIONS                       → capability
func (t *TusdHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	t.handler.ServeHTTP(w, r)
}

// preCreate corre antes de que tusd cree el upload en disco. Extrae
// las claims del JWT, las pasa a Service.PreCreate (que reserva cuota
// + valida), y redirige el binPath del filestore a nuestro layout
// `<staging>/<userID>/<uploadID>/<sanitizedName>`.
//
// La metadata adicional que devolvemos en FileInfoChanges queda
// persistida en el .info de tusd, así que el callback de complete
// la encuentra sin tener que consultar otra estructura.
func (t *TusdHandler) preCreate(hook tushandler.HookEvent) (tushandler.HTTPResponse, tushandler.FileInfoChanges, error) {
	none := tushandler.FileInfoChanges{}

	claims := auth.GetClaims(hook.Context)
	if claims == nil || claims.UserID == "" {
		return tushandler.HTTPResponse{
			StatusCode: http.StatusUnauthorized,
			Body:       "authentication required",
		}, none, fmt.Errorf("unauthenticated upload attempt")
	}

	md := hook.Upload.MetaData
	in := PreCreateInput{
		UserID:        claims.UserID,
		UploadID:      hook.Upload.ID,
		OriginalName:  md["filename"],
		Size:          hook.Upload.Size,
		LibraryIDHint: md["library_id"],
		Subpath:       md["subpath"],
	}

	res, err := t.svc.PreCreate(hook.Context, in)
	if err != nil {
		// 403 cubre cuota / permisos / extensión / nombre. tusd lo
		// propaga al cliente como respuesta del POST de creación.
		return tushandler.HTTPResponse{
			StatusCode: http.StatusForbidden,
			Body:       err.Error(),
		}, none, err
	}

	// Apuntamos el binPath de tusd dentro de nuestro layout. Sin esto
	// tusd escribiría en `<root>/<id>` plano y la pipeline tendría que
	// adivinar dónde lo dejó.
	uploadDir, err := t.svc.staging.UploadDir(claims.UserID, hook.Upload.ID)
	if err != nil {
		// Rollback: liberamos la cuota recién reservada.
		t.svc.PreCreateRollback(hook.Context, claims.UserID, in.Size)
		return tushandler.HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       "could not allocate upload directory",
		}, none, err
	}
	binPath := filepath.Join(uploadDir, res.SanitizedName)

	// Replicamos la metadata del cliente + añadimos lo que la pipeline
	// necesita post-finish (user, started_at, sanitized_name). Si NO
	// rellenamos el campo MetaData de FileInfoChanges, tusd CONSERVA
	// la metadata original — así que aquí lo poblamos completo para
	// que el consumer del CompleteUploads no tenga que volver a inferir.
	now := time.Now().UTC().Format(time.RFC3339Nano)
	return tushandler.HTTPResponse{}, tushandler.FileInfoChanges{
		Storage: map[string]string{
			"Type": "filestore",
			"Path": binPath,
		},
		MetaData: tushandler.MetaData{
			"filename":       md["filename"],
			"sanitized_name": res.SanitizedName,
			"library_id":     md["library_id"],
			"subpath":        md["subpath"],
			"user_id":        claims.UserID,
			"started_at":     now,
		},
	}, nil
}

// preTerminate corre antes de que tusd borre el upload. Solo verifica
// que las claims existen — la propiedad "este upload es tuyo" se
// reforzaría mirando el user_id de la metadata, pero como tusd ya
// requiere el upload-id (no enumerable) y los IDs son entrópicos,
// el filtrado por auth basta en v1. Si más adelante hace falta,
// el chequeo de owner va aquí.
func (t *TusdHandler) preTerminate(hook tushandler.HookEvent) (tushandler.HTTPResponse, error) {
	if claims := auth.GetClaims(hook.Context); claims == nil {
		return tushandler.HTTPResponse{
			StatusCode: http.StatusUnauthorized,
			Body:       "authentication required",
		}, fmt.Errorf("unauthenticated terminate")
	}
	return tushandler.HTTPResponse{}, nil
}

// consumeCompletes drena el canal CompleteUploads y dispara la
// pipeline post-finish en una goroutine SEPARADA por upload. tusd
// ya ha respondido 204 al cliente cuando llega el evento — la
// pipeline corre detrás, y los eventos UploadPhase/UploadDone los
// va viendo el cliente por SSE (/me/events filtra UploadDone +
// UploadError por user_id; UploadPhase es global).
//
// Goroutine separada por upload para que un ffprobe lento de un
// fichero no encole a los demás. Si el proceso cae mientras una
// pipeline está corriendo, el .info del upload sobrevive en
// staging y un GC futuro podría reanudar — fuera de scope v1.
func (t *TusdHandler) consumeCompletes() {
	for evt := range t.handler.CompleteUploads {
		go t.runFinish(evt)
	}
}

func (t *TusdHandler) runFinish(evt tushandler.HookEvent) {
	in := finishInputFromHook(evt, t.svc.staging.Root())
	// El ctx de la pipeline es background — el contexto HTTP de tusd
	// ya ha terminado para cuando llegamos aquí. Si el operador
	// quisiera cancelar todo el pipeline al shutdown, habría que
	// inyectar el ctx del root del binario; v1 no lo hace.
	t.svc.Finish(context.Background(), in)
}

// consumeTerminations drena el canal de DELETE: el cliente abortó.
func (t *TusdHandler) consumeTerminations() {
	for evt := range t.handler.TerminatedUploads {
		evt := evt
		go func() {
			in := finishInputFromHook(evt, t.svc.staging.Root())
			t.svc.Aborted(context.Background(), in)
		}()
	}
}

// finishInputFromHook desempaqueta el HookEvent en el shape que el
// Service espera. binPath cae al default de filestore si la metadata
// `Storage[Path]` falta — defensa por si una versión futura de tusd
// no rellena ese campo en el complete event.
func finishInputFromHook(evt tushandler.HookEvent, stagingRoot string) FinishInput {
	md := evt.Upload.MetaData
	started, _ := time.Parse(time.RFC3339Nano, md["started_at"])
	if started.IsZero() {
		started = time.Now().UTC()
	}

	binPath := ""
	if evt.Upload.Storage != nil {
		binPath = evt.Upload.Storage["Path"]
	}
	if binPath == "" {
		binPath = filepath.Join(stagingRoot, evt.Upload.ID)
	}

	return FinishInput{
		UserID:        md["user_id"],
		UploadID:      evt.Upload.ID,
		OriginalName:  md["filename"],
		SanitizedName: md["sanitized_name"],
		LibraryIDHint: md["library_id"],
		Subpath:       md["subpath"],
		Size:          evt.Upload.Size,
		SourcePath:    binPath,
		StartedAt:     started,
	}
}
