package chunker

import (
	"bytes"
	"fmt"
	"sync"
	"unsafe"

	"github.com/loeredami/ungo"
)

type Memory struct {
	buffer []byte

	allocatedChunks int
	maxSize         int
	chunkSize       int
	rwlock          *sync.Mutex
}

func NewMemory(maxSize MemSize, chunkSize int) *Memory {
	maxSizeInChunks := int(maxSize) / chunkSize
	return &Memory{
		maxSize:   maxSizeInChunks,
		chunkSize: chunkSize,
		rwlock:    &sync.Mutex{},
	}
}

func (m *Memory) Size() int {
	return m.maxSize * m.chunkSize
}

func (m *Memory) ChunkSize() int {
	return m.chunkSize
}

func (m *Memory) MaxSize() int {
	return m.maxSize
}

func (m *Memory) CanAllocate(size int) bool {
	m.rwlock.Lock()
	defer m.rwlock.Unlock()

	memSize := MemSize(size)
	if memSize > MemSize(m.maxSize)*MemSize(m.chunkSize) {
		return false
	}
	headerOffset := MemSize(unsafe.Sizeof(AllocationHeader{}))
	if memSize <= headerOffset {
		return false
	}

	chunksNeeded := (size + m.chunkSize - 1) / m.chunkSize

	for chunk := 0; chunk <= m.maxSize-chunksNeeded; chunk++ {
		addr := MemAddr(chunk * m.chunkSize)
		headerO := m.checkForAndReadAllocationHeader(addr)

		if headerO.HasValue() && headerO.Value().Size > 0 {
			usedChunks := (int(headerO.Value().Size) + m.chunkSize - 1) / m.chunkSize
			if usedChunks < 1 {
				usedChunks = 1
			}
			chunk += usedChunks - 1
			continue
		}

		freeChunks := true
		for c := 0; c < chunksNeeded; c++ {
			checkAddr := MemAddr((chunk + c) * m.chunkSize)
			chkO := m.checkForAndReadAllocationHeader(checkAddr)
			if chkO.HasValue() && chkO.Value().Size > 0 {
				freeChunks = false
				break
			}
		}
		if freeChunks {
			return true
		}
	}
	return false
}

func (m *Memory) GetUsedSize() int {
	m.rwlock.Lock()
	defer m.rwlock.Unlock()
	usedSize := 0
	headerSize := int(unsafe.Sizeof(AllocationHeader{}))
	for chunk := 0; chunk < m.maxSize; chunk++ {
		headerO := m.checkForAndReadAllocationHeader(MemAddr(chunk * m.chunkSize))
		if headerO.HasValue() {
			header := headerO.Value()
			if header.Size == 0 {
				continue
			}
			usedSize += int(header.Size) + headerSize
		}
	}
	return usedSize
}

func (m *Memory) resizeRealAllocation(chunkCount int) ungo.Optional[error] {
	if chunkCount > m.maxSize {
		return ungo.Some(fmt.Errorf("chunk count exceeds max size"))
	}

	targetRealSize := chunkCount * m.chunkSize
	newBuffer := make([]byte, targetRealSize)
	if len(m.buffer) > 0 {
		copy(newBuffer, m.buffer)
	}
	m.buffer = newBuffer
	m.allocatedChunks = chunkCount
	return ungo.None[error]()
}

func (m *Memory) checkForAndReadAllocationHeader(addr MemAddr) ungo.Optional[AllocationHeader] {
	headerSize := int(unsafe.Sizeof(AllocationHeader{}))
	if int(addr) < 0 || int(addr)+headerSize > len(m.buffer) {
		return ungo.None[AllocationHeader]()
	}

	var header AllocationHeader
	headerBytes := (*[unsafe.Sizeof(AllocationHeader{})]byte)(unsafe.Pointer(&header))[:]
	copy(headerBytes, m.buffer[addr:int(addr)+headerSize])

	return ungo.Some(header)
}

func (m *Memory) zeroOutWithHeader(header AllocationHeader, addr MemAddr) ungo.Optional[error] {
	if !header.Locked {
		for i := 0; i < int(header.Size); i++ {
			if int(addr)+i < len(m.buffer) {
				m.buffer[int(addr)+i] = 0
			}
		}
		return ungo.None[error]()
	}
	return ungo.Some(fmt.Errorf("header is locked"))
}

func (m *Memory) writeAllocationHeader(addr MemAddr, size MemSize, locked bool) {
	header := AllocationHeader{Locked: locked, Size: size}
	headerSize := int(unsafe.Sizeof(AllocationHeader{}))

	if int(addr)+headerSize <= len(m.buffer) {
		headerBytes := (*[unsafe.Sizeof(AllocationHeader{})]byte)(unsafe.Pointer(&header))[:]
		copy(m.buffer[addr:int(addr)+headerSize], headerBytes)
	}
}

func (m *Memory) Allocate(size MemSize) ungo.Exception[MemoryPointer] {
	m.rwlock.Lock()
	defer m.rwlock.Unlock()

	if size > MemSize(m.maxSize)*MemSize(m.chunkSize) {
		return ungo.NewException(MemoryPointer{}, fmt.Errorf("size exceeds max size"))
	}

	headerOffset := MemSize(unsafe.Sizeof(AllocationHeader{}))
	if size <= headerOffset {
		return ungo.NewException(MemoryPointer{}, fmt.Errorf("size must be larger than header size"))
	}

	if len(m.buffer) < m.maxSize*m.chunkSize {
		err := m.resizeRealAllocation(m.maxSize)
		if err.HasValue() {
			return ungo.NewException(MemoryPointer{}, err.Value())
		}
	}

	totalNeeded := int(size)
	chunksNeeded := (totalNeeded + m.chunkSize - 1) / m.chunkSize

	for addr := MemAddr(0); int(addr)+chunksNeeded*m.chunkSize <= len(m.buffer); addr += MemAddr(m.chunkSize) {
		headerO := m.checkForAndReadAllocationHeader(addr)

		if !headerO.HasValue() || headerO.Value().Size == 0 {
			freeChunks := true
			for c := 0; c < chunksNeeded; c++ {
				checkAddr := addr + MemAddr(c*m.chunkSize)
				chkO := m.checkForAndReadAllocationHeader(checkAddr)
				if chkO.HasValue() && chkO.Value().Size != 0 {
					freeChunks = false
					break
				}
			}

			if freeChunks {
				newHeader := AllocationHeader{Locked: false, Size: size}
				m.zeroOutWithHeader(newHeader, addr)
				m.writeAllocationHeader(addr, size, false)
				return ungo.NewException(MemoryPointer{Addr: addr, AllocHeader: newHeader}, nil)
			}
		} else {
			h := headerO.Value()
			usedChunks := (int(h.Size) + m.chunkSize - 1) / m.chunkSize
			if usedChunks < 1 {
				usedChunks = 1
			}
			addr += MemAddr((usedChunks - 1) * m.chunkSize)
		}
	}

	return ungo.NewException(MemoryPointer{}, fmt.Errorf("no space available"))
}

func (m *Memory) Free(addr MemAddr) ungo.Exception[MemoryPointer] {
	m.rwlock.Lock()
	defer func() {
		m.cleanTrailingFreeChunks()
		m.rwlock.Unlock()
	}()
	headerO := m.checkForAndReadAllocationHeader(addr)
	if headerO.HasValue() {
		if headerO.Value().Locked {
			return ungo.NewException(MemoryPointer{}, fmt.Errorf("cannot free locked allocation"))
		}

		header := headerO.Value()
		m.zeroOutWithHeader(header, addr)
		m.writeAllocationHeader(addr, 0, false)
		return ungo.NewException(MemoryPointer{Addr: addr, AllocHeader: header}, nil)
	} else {
		return ungo.NewException(MemoryPointer{}, fmt.Errorf("no allocation found at address"))
	}
}

func (m *Memory) Lock(addr MemAddr) ungo.Exception[MemAddr] {
	m.rwlock.Lock()
	defer m.rwlock.Unlock()
	headerO := m.checkForAndReadAllocationHeader(addr)
	if !headerO.HasValue() {
		return ungo.NewException(addr, fmt.Errorf("no allocation found at address"))
	}
	header := headerO.Value()
	header.Locked = true
	m.writeAllocationHeader(addr, header.Size, header.Locked)
	return ungo.NewException(addr, nil)
}

func (m *Memory) Unlock(addr MemAddr) ungo.Exception[MemAddr] {
	m.rwlock.Lock()
	defer m.rwlock.Unlock()
	headerO := m.checkForAndReadAllocationHeader(addr)
	if !headerO.HasValue() {
		return ungo.NewException(addr, fmt.Errorf("no allocation found at address"))
	}
	header := headerO.Value()
	header.Locked = false
	m.writeAllocationHeader(addr, header.Size, header.Locked)
	return ungo.NewException(addr, nil)
}

func (m *Memory) ReadFull(ptr MemoryPointer) []byte {
	m.rwlock.Lock()
	defer m.rwlock.Unlock()
	if ptr.AllocHeader == (AllocationHeader{}) || ptr.AllocHeader.Size == 0 {
		return nil
	}

	headerOffset := int(unsafe.Sizeof(AllocationHeader{}))
	var resultBuffer bytes.Buffer
	for i := headerOffset; i < int(ptr.AllocHeader.Size); i++ {
		idx := int(ptr.Addr) + i
		if idx < len(m.buffer) {
			resultBuffer.WriteByte(m.buffer[idx])
		}
	}
	return resultBuffer.Bytes()
}

func (m *Memory) Read(ptr MemoryPointer, readSize int, offset int) []byte {
	m.rwlock.Lock()
	defer m.rwlock.Unlock()
	if ptr.AllocHeader.Size == 0 {
		return nil
	}

	headerOffset := int(unsafe.Sizeof(AllocationHeader{}))
	payloadSize := int(ptr.AllocHeader.Size) - headerOffset
	if offset < 0 || offset >= payloadSize {
		return nil
	}

	var resultBuffer bytes.Buffer
	for i := 0; i < readSize; i++ {
		if offset+i >= payloadSize {
			break
		}
		idx := int(ptr.Addr) + headerOffset + offset + i
		if idx < len(m.buffer) {
			resultBuffer.WriteByte(m.buffer[idx])
		}
	}
	return resultBuffer.Bytes()
}

func (m *Memory) Write(ptr MemoryPointer, data []byte, offset int) {
	m.rwlock.Lock()
	defer m.rwlock.Unlock()
	if ptr.AllocHeader.Size == 0 {
		return
	}

	headerOffset := int(unsafe.Sizeof(AllocationHeader{}))
	payloadSize := int(ptr.AllocHeader.Size) - headerOffset
	if offset < 0 || offset >= payloadSize {
		return
	}

	for i, b := range data {
		if offset+i >= payloadSize {
			break
		}
		idx := int(ptr.Addr) + headerOffset + offset + i
		if idx < len(m.buffer) {
			m.buffer[idx] = b
		}
	}
}

func (m *Memory) cleanTrailingFreeChunks() {
	for len(m.buffer) >= m.chunkSize {
		addr := MemAddr(len(m.buffer) - m.chunkSize)
		headerO := m.checkForAndReadAllocationHeader(addr)
		if headerO.HasValue() && headerO.Value().Size > 0 {
			break
		}
		m.buffer = m.buffer[:len(m.buffer)-m.chunkSize]
		m.allocatedChunks = len(m.buffer) / m.chunkSize
	}
}

func (m *Memory) Close() {
	// this does nothing, but satisfies the DataStorage interface
}
func (m *Memory) Pointer(addr MemAddr) ungo.Exception[MemoryPointer] {
	m.rwlock.Lock()
	defer m.rwlock.Unlock()

	if int(addr) < 0 || int(addr) >= m.Size() {
		return ungo.NewException(MemoryPointer{}, fmt.Errorf("address out of bounds"))
	}

	startChunkAddr := MemAddr((int(addr) / m.chunkSize) * m.chunkSize)
	for a := startChunkAddr; int(a) >= 0; a -= MemAddr(m.chunkSize) {
		headerO := m.checkForAndReadAllocationHeader(a)
		if headerO.HasValue() && headerO.Value().Size > 0 {
			header := headerO.Value()
			if int(addr) >= int(a) && int(addr) < int(a)+int(header.Size) {
				return ungo.NewException(MemoryPointer{Addr: a, AllocHeader: header}, nil)
			}
		}
	}

	return ungo.NewException(MemoryPointer{}, fmt.Errorf("no allocation found for address"))
}
