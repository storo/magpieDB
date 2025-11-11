package magpie

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// BackupWorker creates periodic database snapshots
type BackupWorker struct {
	interval   time.Duration
	backupDir  string
	maxBackups int
}

// NewBackupWorker creates a new backup worker
func NewBackupWorker(interval time.Duration, backupDir string, maxBackups int) *BackupWorker {
	if maxBackups == 0 {
		maxBackups = 5
	}
	return &BackupWorker{
		interval:   interval,
		backupDir:  backupDir,
		maxBackups: maxBackups,
	}
}

func (w *BackupWorker) Execute(nest *Nest) error {
	timestamp := time.Now().Format("20060102_150405")
	backupPath := filepath.Join(w.backupDir, fmt.Sprintf("magpie_backup_%s.magpie", timestamp))

	if err := os.MkdirAll(w.backupDir, 0755); err != nil {
		return fmt.Errorf("failed to create backup directory: %w", err)
	}

	if err := nest.CreateBackup(backupPath); err != nil {
		return fmt.Errorf("backup creation failed: %w", err)
	}

	return w.rotateBackups()
}

func (w *BackupWorker) Priority() int {
	return 7
}

func (w *BackupWorker) Name() string {
	return "Backup"
}

func (w *BackupWorker) rotateBackups() error {
	files, err := os.ReadDir(w.backupDir)
	if err != nil {
		return fmt.Errorf("failed to read backup directory: %w", err)
	}

	type backupFile struct {
		path    string
		modTime time.Time
	}
	backups := make([]backupFile, 0)

	for _, f := range files {
		if f.IsDir() {
			continue
		}
		ext := filepath.Ext(f.Name())
		if ext != ".magpie" && ext != ".db" {
			continue
		}

		fullPath := filepath.Join(w.backupDir, f.Name())
		info, err := os.Stat(fullPath)
		if err != nil {
			continue
		}

		backups = append(backups, backupFile{
			path:    fullPath,
			modTime: info.ModTime(),
		})
	}

	sort.Slice(backups, func(i, j int) bool {
		return backups[i].modTime.Before(backups[j].modTime)
	})

	if len(backups) > w.maxBackups {
		toRemove := len(backups) - w.maxBackups
		for i := 0; i < toRemove; i++ {
			os.Remove(backups[i].path)
		}
	}

	return nil
}

// CreateBackup creates a consistent snapshot of the database
func (n *Nest) CreateBackup(destPath string) error {
	n.mu.RLock()
	defer n.mu.RUnlock()

	if n.wal != nil {
		if err := n.wal.Flush(); err != nil {
			return fmt.Errorf("WAL flush failed: %w", err)
		}
	}

	if n.storage != nil {
		if err := n.storage.Sync(); err != nil {
			return fmt.Errorf("storage sync failed: %w", err)
		}
	}

	src, err := os.Open(n.path)
	if err != nil {
		return fmt.Errorf("failed to open source: %w", err)
	}
	defer src.Close()

	dst, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("failed to create backup: %w", err)
	}
	defer dst.Close()

	_, err = io.Copy(dst, src)
	if err != nil {
		os.Remove(destPath)
		return fmt.Errorf("backup copy failed: %w", err)
	}

	if err := dst.Sync(); err != nil {
		return fmt.Errorf("backup sync failed: %w", err)
	}

	if err := verifyBackupIntegrity(destPath); err != nil {
		os.Remove(destPath)
		return fmt.Errorf("backup verification failed: %w", err)
	}

	return nil
}

// verifyBackupIntegrity checks if a backup file is valid
func verifyBackupIntegrity(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	headerData := make([]byte, PageSize)
	if _, err := f.Read(headerData); err != nil {
		return err
	}

	header, err := DeserializeDBHeader(headerData)
	if err != nil {
		return fmt.Errorf("invalid header: %w", err)
	}

	// Verify magic
	magic := string(header.Magic[:])
	if magic != "MAGPIE01" {
		return fmt.Errorf("invalid magic: %s", magic)
	}

	return nil
}
