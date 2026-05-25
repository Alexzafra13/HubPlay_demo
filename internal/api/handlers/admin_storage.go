package handlers

import (
	"context"
	"log/slog"
	"net/http"
	"sort"
	"strings"

	"github.com/shirou/gopsutil/v4/disk"

	"hubplay/internal/db"
	librarymodel "hubplay/internal/library/model"
)

// AdminStorageHandler expone el espacio en disco por mount-point +
// el peso de cada biblioteca. Usa gopsutil/disk para los syscalls
// statfs y la query SQL nueva SumItemSizesByLibrary para el peso por
// biblioteca (cero filesystem I/O para la suma — el scanner ya
// captura Size en cada item, ver scanner.go:448).
//
// Por que un endpoint dedicado y no añadirlo a /admin/system/stats:
// el panel /admin/system es "salud del proceso" (CPU/RAM/sesiones/
// DB), refresca cada 30s; storage es "que ocupa esto", cambia solo
// cuando hay scan. Cadencia distinta justifica endpoint distinto -
// el dashboard puede polling 60s en lugar de 30s. Tambien aisla la
// dependencia a gopsutil/disk para que un crash de statfs (raro
// pero posible en bind-mounts exoticos) no rompa /system/stats.
type AdminStorageHandler struct {
	libraries StorageLibraryService
	items     StorageItemReader
	logger    *slog.Logger
}

// StorageLibraryService es el slice estrecho del library.Service que
// el handler necesita. Interface local para mantener el handler
// independiente del gigante LibraryService.
type StorageLibraryService interface {
	List(ctx context.Context) ([]*librarymodel.Library, error)
}

// StorageItemReader expone solo la suma por biblioteca. Mismo
// patron - interface estrecha local. La shape devuelta es la del
// repo (db.LibrarySizeRow), reusada por simplicidad - cero
// conversion en el wiring.
type StorageItemReader interface {
	SumItemSizesByLibrary(ctx context.Context) (map[string]db.LibrarySizeRow, error)
}

func NewAdminStorageHandler(libraries StorageLibraryService, items StorageItemReader, logger *slog.Logger) *AdminStorageHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &AdminStorageHandler{
		libraries: libraries,
		items:     items,
		logger:    logger.With("handler", "admin_storage"),
	}
}

// diskWire es lo que devolvemos al frontend - un disco fisico con
// sus stats agregados + las bibliotecas que viven en el.
type diskWire struct {
	Mount       string             `json:"mount"`
	Filesystem  string             `json:"filesystem,omitempty"`
	TotalBytes  uint64             `json:"total_bytes"`
	UsedBytes   uint64             `json:"used_bytes"`
	FreeBytes   uint64             `json:"free_bytes"`
	UsedPercent float64            `json:"used_percent"`
	Libraries   []libraryDiskWire  `json:"libraries"`
}

type libraryDiskWire struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	ContentType string `json:"content_type"`
	Path        string `json:"path"`
	SizeBytes   int64  `json:"size_bytes"`
	FileCount   int64  `json:"file_count"`
}

// Disks - GET /admin/system/storage/disks. Devuelve un array
// agrupado por mount-point, con cada library asignada al mount
// que la contiene (longest-prefix match sobre disk.Partitions).
//
// Eficiencia:
//   - 1 query SQL (SumItemSizesByLibrary, indexed por library_id).
//   - 1 call a disk.Partitions (cached internamente por gopsutil).
//   - N calls a disk.Usage(mount) - 1 por mount unico. Cada call es
//     un syscall statfs (sub-ms). Tipicamente N = 1-3.
//   - O(L*M) longest-prefix match donde L=libraries, M=mounts.
//     Trivial.
//
// Total: <50ms incluso en servidores con 50+ bibliotecas.
func (h *AdminStorageHandler) Disks(w http.ResponseWriter, r *http.Request) {
	libs, err := h.libraries.List(r.Context())
	if err != nil {
		h.logger.Error("list libraries", "err", err)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL", "failed to list libraries")
		return
	}
	sizes, err := h.items.SumItemSizesByLibrary(r.Context())
	if err != nil {
		h.logger.Error("sum item sizes", "err", err)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL", "failed to compute library sizes")
		return
	}

	// disk.Partitions(false) excluye pseudo-fs (proc, sysfs, tmpfs).
	// Si gopsutil falla aqui (raro - puede pasar en sandbox sin /proc),
	// caemos a una "lista vacia de mounts" y el endpoint devuelve
	// disks=[] en lugar de 500. El admin ve "no hay info de disco"
	// y el resto del panel sigue funcionando.
	parts, err := disk.Partitions(false)
	if err != nil {
		h.logger.Warn("disk.Partitions failed; falling back to empty mount list", "err", err)
		parts = nil
	}
	// Sort por mountpoint length desc para que longest-prefix-match
	// funcione iterando.
	sort.Slice(parts, func(i, j int) bool {
		return len(parts[i].Mountpoint) > len(parts[j].Mountpoint)
	})

	// Acumula libraries por mount.
	type mountBucket struct {
		filesystem string
		libraries  []libraryDiskWire
	}
	buckets := make(map[string]*mountBucket)

	for _, lib := range libs {
		if len(lib.Paths) == 0 {
			// Bibliotecas livetv sin paths (M3U remotos) no tienen
			// concepto "disco". Las saltamos del wire pero su size
			// (rara vez >0 - solo si guarda algo localmente) podria
			// agruparse bajo "remoto" si quisieramos. Para v1 las
			// omitimos completamente.
			continue
		}
		size := sizes[lib.ID]
		for _, libPath := range lib.Paths {
			mount, fs := mountFor(libPath, parts)
			if mount == "" {
				continue
			}
			b, ok := buckets[mount]
			if !ok {
				b = &mountBucket{filesystem: fs}
				buckets[mount] = b
			}
			b.libraries = append(b.libraries, libraryDiskWire{
				ID:          lib.ID,
				Name:        lib.Name,
				ContentType: lib.ContentType,
				Path:        libPath,
				SizeBytes:   size.TotalBytes,
				FileCount:   size.FileCount,
			})
		}
	}

	// Pinta cada mount unico con su usage real.
	disks := make([]diskWire, 0, len(buckets))
	for mount, b := range buckets {
		usage, err := disk.Usage(mount)
		if err != nil {
			h.logger.Warn("disk.Usage failed; skipping mount",
				"mount", mount, "err", err)
			continue
		}
		// Ordenamos las libraries del bucket por size desc para que
		// la mas pesada aparezca primero en la UI.
		sort.Slice(b.libraries, func(i, j int) bool {
			return b.libraries[i].SizeBytes > b.libraries[j].SizeBytes
		})
		disks = append(disks, diskWire{
			Mount:       mount,
			Filesystem:  b.filesystem,
			TotalBytes:  usage.Total,
			UsedBytes:   usage.Used,
			FreeBytes:   usage.Free,
			UsedPercent: usage.UsedPercent,
			Libraries:   b.libraries,
		})
	}
	// Sort disks por used_bytes desc para que el mas "lleno" salga
	// arriba en el dashboard.
	sort.Slice(disks, func(i, j int) bool {
		return disks[i].UsedBytes > disks[j].UsedBytes
	})

	respondData(w, http.StatusOK, map[string]any{
		"disks": disks,
	})
}

// mountFor resuelve la library path al mount que la contiene,
// usando longest-prefix match sobre la lista (ya sorted desc por
// length) de particiones del host.
//
// Devuelve ("", "") si no hay match - raro en Linux porque "/"
// siempre matchea, pero defensivo por si parts viene vacio (sandbox
// donde disk.Partitions fallo).
func mountFor(libPath string, parts []disk.PartitionStat) (mount, fs string) {
	for _, p := range parts {
		if libPath == p.Mountpoint ||
			strings.HasPrefix(libPath, p.Mountpoint+"/") ||
			(p.Mountpoint == "/" && strings.HasPrefix(libPath, "/")) {
			return p.Mountpoint, p.Fstype
		}
	}
	return "", ""
}
