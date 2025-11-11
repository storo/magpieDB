package magpie

import (
	"fmt"
	"os"
	"testing"
)

// Test basic MVCCStorage functionality

func TestMVCCStorageBasic(t *testing.T) {
	// Create temporary file
	tmpfile := tempFilename()
	defer os.Remove(tmpfile)

	file, err := os.OpenFile(tmpfile, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	// Initialize storage
	storage := NewStorage()
	if err := storage.Init(file, PageSize*10); err != nil {
		t.Fatal(err)
	}
	defer storage.Close()

	// Create MVCC storage
	mvccStorage := NewMVCCStorage(storage)

	// Store a version
	v := &MVCCVersion{
		ID:          "vec1",
		Version:     1,
		Vector:      []float32{1.0, 2.0, 3.0},
		Metadata:    map[string]interface{}{"key": "value"},
		CreatedByTx: 100,
		DeletedByTx: 0,
	}

	if err := mvccStorage.StoreVersion(v); err != nil {
		t.Fatal(err)
	}

	// Retrieve it
	versions := mvccStorage.GetAllVersions("vec1")
	if len(versions) != 1 {
		t.Errorf("Expected 1 version, got %d", len(versions))
	}

	if versions[0].ID != "vec1" {
		t.Errorf("Expected ID vec1, got %s", versions[0].ID)
	}

	if versions[0].Version != 1 {
		t.Errorf("Expected version 1, got %d", versions[0].Version)
	}

	if len(versions[0].Vector) != 3 {
		t.Errorf("Expected 3 dimensions, got %d", len(versions[0].Vector))
	}

	if versions[0].Metadata["key"] != "value" {
		t.Errorf("Metadata not preserved")
	}
}

func TestMVCCStorageMultipleVersions(t *testing.T) {
	tmpfile := tempFilename()
	defer os.Remove(tmpfile)

	file, err := os.OpenFile(tmpfile, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	storage := NewStorage()
	if err := storage.Init(file, PageSize*20); err != nil {
		t.Fatal(err)
	}
	defer storage.Close()

	mvccStorage := NewMVCCStorage(storage)

	// Store multiple versions
	for i := 1; i <= 5; i++ {
		v := &MVCCVersion{
			ID:          "vec1",
			Version:     uint64(i),
			Vector:      []float32{float32(i)},
			CreatedByTx: uint64(i * 100),
			DeletedByTx: 0,
		}
		if err := mvccStorage.StoreVersion(v); err != nil {
			t.Fatal(err)
		}
	}

	// Retrieve all versions
	versions := mvccStorage.GetAllVersions("vec1")
	if len(versions) != 5 {
		t.Errorf("Expected 5 versions, got %d", len(versions))
	}

	// Verify newest first
	if versions[0].Version != 5 {
		t.Errorf("Expected newest version 5 first, got %d", versions[0].Version)
	}
}

func TestMVCCStoragePersistence(t *testing.T) {
	tmpfile := tempFilename()
	defer os.Remove(tmpfile)

	// Phase 1: Write
	{
		file, err := os.OpenFile(tmpfile, os.O_RDWR|os.O_CREATE, 0644)
		if err != nil {
			t.Fatal(err)
		}

		storage := NewStorage()
		if err := storage.Init(file, PageSize*20); err != nil {
			t.Fatal(err)
		}

		mvccStorage := NewMVCCStorage(storage)

		for i := 1; i <= 3; i++ {
			v := &MVCCVersion{
				ID:          fmt.Sprintf("vec%d", i),
				Version:     1,
				Vector:      []float32{float32(i), float32(i * 2)},
				CreatedByTx: uint64(i),
			}
			if err := mvccStorage.StoreVersion(v); err != nil {
				t.Fatal(err)
			}
		}

		// Sync to disk before closing
		if err := storage.Sync(); err != nil {
			t.Fatal(err)
		}

		storage.Close()
		file.Close()
	}

	// Phase 2: Reload
	{
		file, err := os.OpenFile(tmpfile, os.O_RDWR, 0644)
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()

		// Get actual file size
		info, err := file.Stat()
		if err != nil {
			t.Fatal(err)
		}

		storage := NewStorage()
		if err := storage.Init(file, info.Size()); err != nil {
			t.Fatal(err)
		}
		defer storage.Close()

		mvccStorage := NewMVCCStorage(storage)

		t.Logf("PageCount before load: %d", storage.PageCount())

		if err := mvccStorage.LoadVersionChains(); err != nil {
			t.Fatal(err)
		}

		t.Logf("VectorCount after load: %d", mvccStorage.GetVectorCount())
		t.Logf("VersionCount after load: %d", mvccStorage.GetVersionCount())

		// Verify all vectors are loaded
		if mvccStorage.GetVectorCount() != 3 {
			t.Errorf("Expected 3 vectors, got %d", mvccStorage.GetVectorCount())
		}

		// Verify specific vector
		versions := mvccStorage.GetAllVersions("vec2")
		if len(versions) != 1 {
			t.Errorf("Expected 1 version for vec2, got %d", len(versions))
		}

		if versions[0].Vector[0] != 2.0 {
			t.Errorf("Expected vector[0]=2.0, got %f", versions[0].Vector[0])
		}
	}
}

func TestMVCCStorageGC(t *testing.T) {
	tmpfile := tempFilename()
	defer os.Remove(tmpfile)

	file, err := os.OpenFile(tmpfile, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	storage := NewStorage()
	if err := storage.Init(file, PageSize*30); err != nil {
		t.Fatal(err)
	}
	defer storage.Close()

	mvccStorage := NewMVCCStorage(storage)

	// Store 10 versions (mark older ones as deleted)
	for i := 1; i <= 10; i++ {
		deletedBy := uint64(0)
		if i < 5 {
			// Mark versions 1-4 as deleted by tx 45
			deletedBy = 45
		}
		v := &MVCCVersion{
			ID:          "vec1",
			Version:     uint64(i),
			Vector:      []float32{float32(i)},
			CreatedByTx: uint64(i * 10),
			DeletedByTx: deletedBy,
		}
		if err := mvccStorage.StoreVersion(v); err != nil {
			t.Fatal(err)
		}
	}

	// GC versions older than txID 50
	// Should remove versions 1-4 (created at 10,20,30,40 and deleted at 45)
	removed := mvccStorage.GCVersionsOlderThan(50)
	if removed < 3 {
		t.Errorf("Expected to remove at least 3 versions, removed %d", removed)
	}

	// Verify we still have versions
	versions := mvccStorage.GetAllVersions("vec1")
	if len(versions) == 0 {
		t.Error("GC removed all versions")
	}
}

func TestMVCCStorageGetLatest(t *testing.T) {
	tmpfile := tempFilename()
	defer os.Remove(tmpfile)

	file, err := os.OpenFile(tmpfile, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	storage := NewStorage()
	if err := storage.Init(file, PageSize*10); err != nil {
		t.Fatal(err)
	}
	defer storage.Close()

	mvccStorage := NewMVCCStorage(storage)

	// Store versions out of order
	for _, ver := range []uint64{3, 1, 5, 2, 4} {
		v := &MVCCVersion{
			ID:          "vec1",
			Version:     ver,
			Vector:      []float32{float32(ver)},
			CreatedByTx: ver * 10,
		}
		if err := mvccStorage.StoreVersion(v); err != nil {
			t.Fatal(err)
		}
	}

	// Get latest should return version 5
	latest := mvccStorage.GetLatestVersion("vec1")
	if latest == nil {
		t.Fatal("Latest version is nil")
	}

	if latest.Version != 5 {
		t.Errorf("Expected latest version 5, got %d", latest.Version)
	}
}

func TestMVCCStorageGetSpecific(t *testing.T) {
	tmpfile := tempFilename()
	defer os.Remove(tmpfile)

	file, err := os.OpenFile(tmpfile, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	storage := NewStorage()
	if err := storage.Init(file, PageSize*10); err != nil {
		t.Fatal(err)
	}
	defer storage.Close()

	mvccStorage := NewMVCCStorage(storage)

	// Store multiple versions
	for i := 1; i <= 5; i++ {
		v := &MVCCVersion{
			ID:          "vec1",
			Version:     uint64(i),
			Vector:      []float32{float32(i * 10)},
			CreatedByTx: uint64(i * 100),
		}
		if err := mvccStorage.StoreVersion(v); err != nil {
			t.Fatal(err)
		}
	}

	// Get specific version 3
	v3 := mvccStorage.GetVersion("vec1", 3)
	if v3 == nil {
		t.Fatal("Version 3 not found")
	}

	if v3.Version != 3 {
		t.Errorf("Expected version 3, got %d", v3.Version)
	}

	if v3.Vector[0] != 30.0 {
		t.Errorf("Expected vector value 30, got %f", v3.Vector[0])
	}
}

func TestMVCCStorageDeleted(t *testing.T) {
	tmpfile := tempFilename()
	defer os.Remove(tmpfile)

	file, err := os.OpenFile(tmpfile, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	storage := NewStorage()
	if err := storage.Init(file, PageSize*10); err != nil {
		t.Fatal(err)
	}
	defer storage.Close()

	mvccStorage := NewMVCCStorage(storage)

	// Store deleted version
	v := &MVCCVersion{
		ID:          "vec1",
		Version:     1,
		Vector:      []float32{1, 2, 3},
		CreatedByTx: 100,
		DeletedByTx: 200, // Deleted
	}

	if err := mvccStorage.StoreVersion(v); err != nil {
		t.Fatal(err)
	}

	// Verify DeletedByTx is preserved
	versions := mvccStorage.GetAllVersions("vec1")
	if len(versions) != 1 {
		t.Error("Should store deleted version")
	}

	if versions[0].DeletedByTx != 200 {
		t.Errorf("Expected DeletedByTx=200, got %d", versions[0].DeletedByTx)
	}
}
