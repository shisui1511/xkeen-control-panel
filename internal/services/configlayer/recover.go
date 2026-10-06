package configlayer

import "time"

// RecoverJournal — заглушка RED-коммита.
func RecoverJournal(store *Store, roots Roots) (bool, error) { return false, nil }

// CleanupStale — заглушка RED-коммита.
func CleanupStale(dataDir string, roots Roots, now time.Time) error { return nil }
