package chunker

import "github.com/loeredami/ungo"

type DataStorage interface {
	Size() int
	ChunkSize() int
	MaxSize() int
	CanAllocate(size int) bool
	GetUsedSize() int
	Allocate(size MemSize) ungo.Exception[MemoryPointer]
	Free(addr MemAddr) ungo.Exception[MemoryPointer]
	Lock(addr MemAddr) ungo.Exception[MemAddr]
	Unlock(addr MemAddr) ungo.Exception[MemAddr]
	ReadFull(ptr MemoryPointer) []byte
	Read(ptr MemoryPointer, readSize int, offset int) []byte
	Write(ptr MemoryPointer, data []byte, offset int)
}
