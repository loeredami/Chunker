package chunker

import (
	"fmt"
	"os"
	"sync"
	"unsafe"

	"github.com/loeredami/ungo"
)

type SwapMemory struct {
	file            *os.File
	fileSize        int64
	allocatedChunks int
	maxSize         int
	chunkSize       int
	rwlock          *sync.Mutex
}

func NewSwapMemory(filePath string, maxSize MemSize, chunkSize int) (*SwapMemory, error) {
	file, err := os.OpenFile(filePath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0666)
	if err != nil {
		return nil, err
	}
	maxSizeInChunks := int(maxSize) / chunkSize
	return &SwapMemory{
		file:      file,
		maxSize:   maxSizeInChunks,
		chunkSize: chunkSize,
		rwlock:    &sync.Mutex{},
	}, nil
}

func (s *SwapMemory) Size() int {
	return s.maxSize * s.chunkSize
}

func (s *SwapMemory) ChunkSize() int {
	return s.chunkSize
}

func (s *SwapMemory) MaxSize() int {
	return s.maxSize
}

func (s *SwapMemory) CanAllocate(size int) bool {
	s.rwlock.Lock()
	defer s.rwlock.Unlock()

	memSize := MemSize(size)
	if memSize > MemSize(s.maxSize)*MemSize(s.chunkSize) {
		return false
	}
	headerOffset := MemSize(unsafe.Sizeof(AllocationHeader{}))
	if memSize <= headerOffset {
		return false
	}

	chunksNeeded := (size + s.chunkSize - 1) / s.chunkSize

	for chunk := 0; chunk <= s.maxSize-chunksNeeded; chunk++ {
		addr := MemAddr(chunk * s.chunkSize)
		headerO := s.checkForAndReadAllocationHeader(addr)

		if headerO.HasValue() && headerO.Value().size > 0 {
			usedChunks := (int(headerO.Value().size) + s.chunkSize - 1) / s.chunkSize
			if usedChunks < 1 {
				usedChunks = 1
			}
			chunk += usedChunks - 1
			continue
		}

		freeChunks := true
		for c := 0; c < chunksNeeded; c++ {
			checkAddr := MemAddr((chunk + c) * s.chunkSize)
			chkO := s.checkForAndReadAllocationHeader(checkAddr)
			if chkO.HasValue() && chkO.Value().size > 0 {
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

func (s *SwapMemory) GetUsedSize() int {
	s.rwlock.Lock()
	defer s.rwlock.Unlock()
	usedSize := 0
	headerSize := int(unsafe.Sizeof(AllocationHeader{}))
	for chunk := 0; chunk < s.maxSize; chunk++ {
		headerO := s.checkForAndReadAllocationHeader(MemAddr(chunk * s.chunkSize))
		if headerO.HasValue() {
			header := headerO.Value()
			if header.size == 0 {
				continue
			}
			usedSize += int(header.size) + headerSize
		}
	}
	return usedSize
}

func (s *SwapMemory) ensureFileSize(chunkCount int) ungo.Optional[error] {
	if chunkCount > s.maxSize {
		return ungo.Some(fmt.Errorf("chunk count exceeds max size"))
	}

	targetRealSize := int64(chunkCount * s.chunkSize)
	if targetRealSize > s.fileSize {
		if err := s.file.Truncate(targetRealSize); err != nil {
			return ungo.Some(err)
		}
		s.fileSize = targetRealSize
	}
	s.allocatedChunks = chunkCount
	return ungo.None[error]()
}

func (s *SwapMemory) checkForAndReadAllocationHeader(addr MemAddr) ungo.Optional[AllocationHeader] {
	headerSize := int(unsafe.Sizeof(AllocationHeader{}))
	if int64(addr) < 0 || int64(addr)+int64(headerSize) > s.fileSize {
		return ungo.None[AllocationHeader]()
	}

	var header AllocationHeader
	headerBytes := (*[unsafe.Sizeof(AllocationHeader{})]byte)(unsafe.Pointer(&header))[:]
	_, err := s.file.ReadAt(headerBytes, int64(addr))
	if err != nil {
		return ungo.None[AllocationHeader]()
	}

	return ungo.Some(header)
}

func (s *SwapMemory) zeroOutWithHeader(header AllocationHeader, addr MemAddr) ungo.Optional[error] {
	if !header.locked {
		zeroBytes := make([]byte, header.size)
		_, err := s.file.WriteAt(zeroBytes, int64(addr))
		if err != nil {
			return ungo.Some(err)
		}
		return ungo.None[error]()
	}
	return ungo.Some(fmt.Errorf("header is locked"))
}

func (s *SwapMemory) writeAllocationHeader(addr MemAddr, size MemSize, locked bool) {
	header := AllocationHeader{locked: locked, size: size}
	headerSize := int(unsafe.Sizeof(AllocationHeader{}))

	if int64(addr)+int64(headerSize) <= s.fileSize {
		headerBytes := (*[unsafe.Sizeof(AllocationHeader{})]byte)(unsafe.Pointer(&header))[:]
		s.file.WriteAt(headerBytes, int64(addr))
	}
}

func (s *SwapMemory) Allocate(size MemSize) ungo.Exception[MemoryPointer] {
	s.rwlock.Lock()
	defer s.rwlock.Unlock()

	if size > MemSize(s.maxSize)*MemSize(s.chunkSize) {
		return ungo.NewException(MemoryPointer{}, fmt.Errorf("size exceeds max size"))
	}

	headerOffset := MemSize(unsafe.Sizeof(AllocationHeader{}))
	if size <= headerOffset {
		return ungo.NewException(MemoryPointer{}, fmt.Errorf("size must be larger than header size"))
	}

	if s.fileSize < int64(s.maxSize*s.chunkSize) {
		err := s.ensureFileSize(s.maxSize)
		if err.HasValue() {
			return ungo.NewException(MemoryPointer{}, err.Value())
		}
	}

	totalNeeded := int(size)
	chunksNeeded := (totalNeeded + s.chunkSize - 1) / s.chunkSize

	for addr := MemAddr(0); int64(addr)+int64(chunksNeeded*s.chunkSize) <= s.fileSize; addr += MemAddr(s.chunkSize) {
		headerO := s.checkForAndReadAllocationHeader(addr)

		if !headerO.HasValue() || headerO.Value().size == 0 {
			freeChunks := true
			for c := 0; c < chunksNeeded; c++ {
				checkAddr := addr + MemAddr(c*s.chunkSize)
				chkO := s.checkForAndReadAllocationHeader(checkAddr)
				if chkO.HasValue() && chkO.Value().size != 0 {
					freeChunks = false
					break
				}
			}

			if freeChunks {
				newHeader := AllocationHeader{locked: false, size: size}
				s.zeroOutWithHeader(newHeader, addr)
				s.writeAllocationHeader(addr, size, false)
				return ungo.NewException(MemoryPointer{addr: addr, allocHeader: newHeader}, nil)
			}
		} else {
			h := headerO.Value()
			usedChunks := (int(h.size) + s.chunkSize - 1) / s.chunkSize
			if usedChunks < 1 {
				usedChunks = 1
			}
			addr += MemAddr((usedChunks - 1) * s.chunkSize)
		}
	}

	return ungo.NewException(MemoryPointer{}, fmt.Errorf("no space available"))
}

func (s *SwapMemory) Free(addr MemAddr) ungo.Exception[MemoryPointer] {
	s.rwlock.Lock()
	defer func() {
		s.cleanTrailingFreeChunks()
		s.rwlock.Unlock()
	}()
	headerO := s.checkForAndReadAllocationHeader(addr)
	if headerO.HasValue() {
		if headerO.Value().locked {
			return ungo.NewException(MemoryPointer{addr: addr, allocHeader: headerO.Value()}, fmt.Errorf("cannot free locked allocation"))
		}

		s.zeroOutWithHeader(headerO.Value(), addr)
		s.writeAllocationHeader(addr, 0, false)
	} else {
		return ungo.NewException(MemoryPointer{addr: addr}, fmt.Errorf("no allocation found at address"))
	}
	return ungo.NewException(MemoryPointer{addr: addr, allocHeader: headerO.Value()}, nil)
}

func (s *SwapMemory) Lock(addr MemAddr) ungo.Exception[MemAddr] {
	s.rwlock.Lock()
	defer s.rwlock.Unlock()
	headerO := s.checkForAndReadAllocationHeader(addr)
	if !headerO.HasValue() {
		return ungo.NewException(addr, fmt.Errorf("no allocation found at address"))
	}
	header := headerO.Value()
	header.locked = true
	s.writeAllocationHeader(addr, header.size, header.locked)
	return ungo.NewException(addr, nil)
}

func (s *SwapMemory) Unlock(addr MemAddr) ungo.Exception[MemAddr] {
	s.rwlock.Lock()
	defer s.rwlock.Unlock()
	headerO := s.checkForAndReadAllocationHeader(addr)
	if !headerO.HasValue() {
		return ungo.NewException(addr, fmt.Errorf("no allocation found at address"))
	}
	header := headerO.Value()
	header.locked = false
	s.writeAllocationHeader(addr, header.size, header.locked)
	return ungo.NewException(addr, nil)
}

func (s *SwapMemory) ReadFull(ptr MemoryPointer) []byte {
	s.rwlock.Lock()
	defer s.rwlock.Unlock()
	if ptr.allocHeader == (AllocationHeader{}) || ptr.allocHeader.size == 0 {
		return nil
	}

	headerOffset := int(unsafe.Sizeof(AllocationHeader{}))
	payloadSize := int(ptr.allocHeader.size) - headerOffset
	if payloadSize <= 0 {
		return nil
	}

	buf := make([]byte, payloadSize)
	_, err := s.file.ReadAt(buf, int64(ptr.addr)+int64(headerOffset))
	if err != nil {
		return nil
	}
	return buf
}

func (s *SwapMemory) Read(ptr MemoryPointer, readSize int, offset int) []byte {
	s.rwlock.Lock()
	defer s.rwlock.Unlock()
	if ptr.allocHeader.size == 0 {
		return nil
	}

	headerOffset := int(unsafe.Sizeof(AllocationHeader{}))
	payloadSize := int(ptr.allocHeader.size) - headerOffset
	if offset < 0 || offset >= payloadSize {
		return nil
	}

	actualReadSize := readSize
	if offset+actualReadSize > payloadSize {
		actualReadSize = payloadSize - offset
	}
	if actualReadSize <= 0 {
		return nil
	}

	buf := make([]byte, actualReadSize)
	_, err := s.file.ReadAt(buf, int64(ptr.addr)+int64(headerOffset)+int64(offset))
	if err != nil {
		return nil
	}
	return buf
}

func (s *SwapMemory) Write(ptr MemoryPointer, data []byte, offset int) {
	s.rwlock.Lock()
	defer s.rwlock.Unlock()
	if ptr.allocHeader.size == 0 {
		return
	}

	headerOffset := int(unsafe.Sizeof(AllocationHeader{}))
	payloadSize := int(ptr.allocHeader.size) - headerOffset
	if offset < 0 || offset >= payloadSize {
		return
	}

	actualWriteSize := len(data)
	if offset+actualWriteSize > payloadSize {
		actualWriteSize = payloadSize - offset
	}
	if actualWriteSize <= 0 {
		return
	}

	s.file.WriteAt(data[:actualWriteSize], int64(ptr.addr)+int64(headerOffset)+int64(offset))
}

func (s *SwapMemory) cleanTrailingFreeChunks() {
	for s.fileSize >= int64(s.chunkSize) {
		addr := MemAddr(s.fileSize - int64(s.chunkSize))
		headerO := s.checkForAndReadAllocationHeader(addr)
		if headerO.HasValue() && headerO.Value().size > 0 {
			break
		}
		s.fileSize -= int64(s.chunkSize)
		s.file.Truncate(s.fileSize)
		s.allocatedChunks = int(s.fileSize) / s.chunkSize
	}
}

func (s *SwapMemory) Close() {
	s.rwlock.Lock()
	defer s.rwlock.Unlock()
	if s.file != nil {
		s.file.Close()
		s.file = nil
	}
}
