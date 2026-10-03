package chunker

type MemAddr uintptr
type MemSize uint64

const (
	KB MemSize = 1 << 10
	MB MemSize = 1 << 20
	GB MemSize = 1 << 30
	TB MemSize = 1 << 40
)

type AllocationHeader struct {
	locked bool
	size   MemSize
}

type MemoryPointer struct {
	addr        MemAddr
	allocHeader AllocationHeader
}
