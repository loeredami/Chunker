package chunker

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"unsafe"
)

func TestSwapMemoryAllocationAndLifecycle(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "swap_test_*.bin")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	tmpName := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpName)

	mem, err := NewSwapMemory(tmpName, 1024, 32)
	if err != nil {
		t.Fatalf("Failed to create SwapMemory: %v", err)
	}
	defer mem.Close()

	headerSize := MemSize(unsafe.Sizeof(AllocationHeader{}))
	allocSize := MemSize(64)

	ptrE := mem.Allocate(allocSize)
	if ptrE.Error != nil {
		t.Fatalf("Failed to allocate: %v", ptrE.Error)
	}

	ptr := ptrE.Value
	if ptr.allocHeader.size != allocSize {
		t.Errorf("Expected size %d, got %d", allocSize, ptr.allocHeader.size)
	}

	payloadSize := int(allocSize) - int(headerSize)
	data := []byte("hello swap")

	if len(data) <= payloadSize {
		mem.Write(ptr, data, 0)

		readData := mem.Read(ptr, len(data), 0)
		if !bytes.Equal(data, readData) {
			t.Errorf("Expected %s, got %s", data, readData)
		}

		fullData := mem.ReadFull(ptr)
		if !bytes.HasPrefix(fullData, data) {
			t.Errorf("ReadFull did not contain written data")
		}
	}

	freeE := mem.Free(ptr.addr)
	if freeE.Error != nil {
		t.Fatalf("Failed to free memory: %v", freeE.Error)
	}
}

func TestSwapMemoryBounds(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "swap_test_*.bin")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	tmpName := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpName)

	mem, err := NewSwapMemory(tmpName, 128, 32)
	if err != nil {
		t.Fatalf("Failed to create SwapMemory: %v", err)
	}
	defer mem.Close()

	ptrE := mem.Allocate(256)
	if ptrE.Error == nil || !strings.Contains(ptrE.Error.Error(), "size exceeds max size") {
		t.Errorf("Expected error allocating beyond max size")
	}
}

func TestSwapTrailingDeallocationAndCleanup(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "swap_test_*.bin")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	tmpName := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpName)

	mem, err := NewSwapMemory(tmpName, 256, 32)
	if err != nil {
		t.Fatalf("Failed to create SwapMemory: %v", err)
	}
	defer mem.Close()

	_ = mem.Allocate(20)
	_ = mem.Allocate(20)
	p3 := mem.Allocate(20)

	initialSize := mem.fileSize

	freeE := mem.Free(p3.Value.addr)
	if freeE.Error != nil {
		t.Fatalf("Failed to free trailing block: %v", freeE.Error)
	}

	if mem.fileSize >= initialSize {
		t.Errorf("Expected file size to shrink after freeing trailing chunk, current size: %d", mem.fileSize)
	}
}
