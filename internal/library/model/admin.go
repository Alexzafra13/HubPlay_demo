package model

// DailyWatchBucket es un punto del gráfico diario de actividad
// del panel admin.
type DailyWatchBucket struct {
	Date         string
	WatchMinutes int
	SessionCount int
}

// TopItemRow es una fila del ranking "más vistos" del panel admin.
type TopItemRow struct {
	ID        string
	Type      string
	Title     string
	PlayCount int
}

// LibrarySizeRow resume el espacio en disco de una biblioteca.
type LibrarySizeRow struct {
	LibraryID  string
	TotalBytes int64
	FileCount  int64
}
