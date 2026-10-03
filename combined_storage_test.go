package chunker

import (
	"bytes"
	"testing"
)

func TestCombinedStorageAllocationAndLifecycle(t *testing.T) {
	mem1 := NewMemory(64, 32)  // 2 chunks (64 bytes)
	mem2 := NewMemory(128, 32) // 4 chunks (128 bytes)

	combined := NewCombinedStorage(mem1, mem2)
	defer combined.Close()

	if combined.Size() != 192 {
		t.Errorf("Expected combined size 192, got %d", combined.Size())
	}

	allocSize := MemSize(64)

	// Allocate filling mem1
	ptr1E := combined.Allocate(allocSize)
	if ptr1E.Error != nil {
		t.Fatalf("Failed to allocate in combined storage 1: %v", ptr1E.Error)
	}

	// Next allocation should overflow into mem2
	ptr2E := combined.Allocate(allocSize)
	if ptr2E.Error != nil {
		t.Fatalf("Failed to allocate in combined storage 2: %v", ptr2E.Error)
	}

	// Verify that the second allocation address is offset by mem1's size (64 bytes)
	if ptr2E.Value.addr < 64 {
		t.Errorf("Expected second allocation address to be offset past 64, got %d", ptr2E.Value.addr)
	}

	// Test writing and reading across combined storage
	data := []byte("combined test")
	combined.Write(ptr2E.Value, data, 0)

	readData := combined.Read(ptr2E.Value, len(data), 0)
	if !bytes.Equal(data, readData) {
		t.Errorf("Expected %s, got %s", data, readData)
	}

	// Test freeing
	freeE := combined.Free(ptr2E.Value.addr)
	if freeE.Error != nil {
		t.Fatalf("Failed to free in combined storage: %v", freeE.Error)
	}
}
