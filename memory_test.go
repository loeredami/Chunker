package chunker

import (
	"bytes"
	"strings"
	"testing"
	"unsafe"
)

func TestMemoryAllocationAndLifecycle(t *testing.T) {
	mem := NewMemory(1024, 32)
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
	if ptr.allocHeader.locked {
		t.Errorf("Expected new allocation to be unlocked")
	}

	payloadSize := int(allocSize) - int(headerSize)
	data := []byte("hello world")

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

	lockE := mem.Lock(ptr.addr)
	if lockE.Error != nil {
		t.Fatalf("Failed to lock memory: %v", lockE.Error)
	}

	freeE := mem.Free(ptr.addr)
	if freeE.Error == nil {
		t.Errorf("Expected error when freeing locked memory")
	}

	unlockE := mem.Unlock(ptr.addr)
	if unlockE.Error != nil {
		t.Fatalf("Failed to unlock memory: %v", unlockE.Error)
	}

	freeE = mem.Free(ptr.addr)
	if freeE.Error != nil {
		t.Fatalf("Failed to free memory: %v", freeE.Error)
	}

	ptrE2 := mem.Allocate(allocSize)
	if ptrE2.Error != nil {
		t.Fatalf("Failed to re-allocate: %v", ptrE2.Error)
	}
	if ptrE2.Value.addr != ptr.addr {
		t.Errorf("Expected memory to be reused at addr %d, got %d", ptr.addr, ptrE2.Value.addr)
	}
}

func TestMemoryBounds(t *testing.T) {
	mem := NewMemory(128, 32) // max 4 chunks (128 bytes total)

	ptrE := mem.Allocate(256)
	if ptrE.Error == nil || !strings.Contains(ptrE.Error.Error(), "size exceeds max size") {
		t.Errorf("Expected error allocating beyond max size")
	}

	p1 := mem.Allocate(20)
	p2 := mem.Allocate(20)
	p3 := mem.Allocate(20)
	p4 := mem.Allocate(20)
	if p1.Error != nil || p2.Error != nil || p3.Error != nil || p4.Error != nil {
		t.Fatalf("Failed to fill memory: p1=%v, p2=%v, p3=%v, p4=%v", p1.Error, p2.Error, p3.Error, p4.Error)
	}

	p5 := mem.Allocate(20)
	if p5.Error == nil {
		t.Errorf("Expected error when memory is full")
	}
}

func TestCanAllocateAndGetUsedSize(t *testing.T) {
	mem := NewMemory(128, 32)

	if !mem.CanAllocate(20) {
		t.Errorf("Expected CanAllocate to be true on empty memory")
	}

	p1 := mem.Allocate(20)
	if p1.Error != nil {
		t.Fatalf("Failed to allocate: %v", p1.Error)
	}

	used := mem.GetUsedSize()
	headerSize := int(unsafe.Sizeof(AllocationHeader{}))
	expectedUsed := 20 + headerSize
	if used != expectedUsed {
		t.Errorf("Expected used size %d, got %d", expectedUsed, used)
	}

	// Fill remaining
	_ = mem.Allocate(20)
	_ = mem.Allocate(20)
	_ = mem.Allocate(20)

	if mem.CanAllocate(20) {
		t.Errorf("Expected CanAllocate to be false when memory is full")
	}
}

func TestTrailingDeallocationAndCleanup(t *testing.T) {
	mem := NewMemory(256, 32)

	p1 := mem.Allocate(20)
	p2 := mem.Allocate(20)
	p3 := mem.Allocate(20)

	if p1.Error != nil || p2.Error != nil || p3.Error != nil {
		t.Fatalf("Setup allocations failed")
	}

	initialLen := len(mem.buffer)

	// Freeing p3 (the trailing block) should trigger cleanTrailingFreeChunks and shrink the buffer
	freeE := mem.Free(p3.Value.addr)
	if freeE.Error != nil {
		t.Fatalf("Failed to free trailing block: %v", freeE.Error)
	}

	if len(mem.buffer) >= initialLen {
		t.Errorf("Expected buffer to shrink after freeing trailing chunk, current len: %d", len(mem.buffer))
	}
}
