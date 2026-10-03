package chunker

import (
	"fmt"
	"sync"

	"github.com/loeredami/ungo"
)

type CombinedStorage struct {
	storages []DataStorage
	rwlock   *sync.Mutex
}

func NewCombinedStorage(storages ...DataStorage) *CombinedStorage {
	return &CombinedStorage{
		storages: storages,
		rwlock:   &sync.Mutex{},
	}
}

func (cs *CombinedStorage) AddStorage(storage DataStorage) {
	cs.rwlock.Lock()
	defer cs.rwlock.Unlock()
	cs.storages = append(cs.storages, storage)
}

func (cs *CombinedStorage) Size() int {
	cs.rwlock.Lock()
	defer cs.rwlock.Unlock()
	total := 0
	for _, s := range cs.storages {
		total += s.Size()
	}
	return total
}

func (cs *CombinedStorage) ChunkSize() int {
	cs.rwlock.Lock()
	defer cs.rwlock.Unlock()
	if len(cs.storages) == 0 {
		return 0
	}
	return cs.storages[0].ChunkSize()
}

func (cs *CombinedStorage) MaxSize() int {
	cs.rwlock.Lock()
	defer cs.rwlock.Unlock()
	total := 0
	for _, s := range cs.storages {
		total += s.MaxSize()
	}
	return total
}

func (cs *CombinedStorage) CanAllocate(size int) bool {
	cs.rwlock.Lock()
	defer cs.rwlock.Unlock()
	for _, s := range cs.storages {
		if s.CanAllocate(size) {
			return true
		}
	}
	return false
}

func (cs *CombinedStorage) GetUsedSize() int {
	cs.rwlock.Lock()
	defer cs.rwlock.Unlock()
	total := 0
	for _, s := range cs.storages {
		total += s.GetUsedSize()
	}
	return total
}

func (cs *CombinedStorage) findStorageAndOffset(addr MemAddr) (DataStorage, MemAddr, ungo.Optional[error]) {
	offset := MemAddr(0)
	for _, s := range cs.storages {
		sSize := MemAddr(s.Size())
		if addr >= offset && addr < offset+sSize {
			return s, offset, ungo.None[error]()
		}
		offset += sSize
	}
	return nil, 0, ungo.Some(fmt.Errorf("address out of bounds"))
}

func (cs *CombinedStorage) Allocate(size MemSize) ungo.Exception[MemoryPointer] {
	cs.rwlock.Lock()
	defer cs.rwlock.Unlock()

	offset := MemAddr(0)
	for _, s := range cs.storages {
		sSize := MemAddr(s.Size())
		if s.CanAllocate(int(size)) {
			ptrE := s.Allocate(size)
			if ptrE.Error == nil {
				translatedPtr := MemoryPointer{
					Addr:        ptrE.Value.Addr + offset,
					AllocHeader: ptrE.Value.AllocHeader,
				}
				return ungo.NewException(translatedPtr, nil)
			}
		}
		offset += sSize
	}

	return ungo.NewException(MemoryPointer{}, fmt.Errorf("no space available in any attached storage"))
}

func (cs *CombinedStorage) Free(addr MemAddr) ungo.Exception[MemAddr] {
	cs.rwlock.Lock()
	defer cs.rwlock.Unlock()

	s, offset, errOpt := cs.findStorageAndOffset(addr)
	if errOpt.HasValue() {
		return ungo.NewException(addr, errOpt.Value())
	}

	resE := s.Free(addr - offset)
	if resE.Error != nil {
		return ungo.NewException(addr, resE.Error)
	}
	return ungo.NewException(resE.Value.Addr+offset, nil)
}

func (cs *CombinedStorage) Lock(addr MemAddr) ungo.Exception[MemAddr] {
	cs.rwlock.Lock()
	defer cs.rwlock.Unlock()

	s, offset, errOpt := cs.findStorageAndOffset(addr)
	if errOpt.HasValue() {
		return ungo.NewException(addr, errOpt.Value())
	}

	resE := s.Lock(addr - offset)
	if resE.Error != nil {
		return ungo.NewException(addr, resE.Error)
	}
	return ungo.NewException(resE.Value+offset, nil)
}

func (cs *CombinedStorage) Unlock(addr MemAddr) ungo.Exception[MemAddr] {
	cs.rwlock.Lock()
	defer cs.rwlock.Unlock()

	s, offset, errOpt := cs.findStorageAndOffset(addr)
	if errOpt.HasValue() {
		return ungo.NewException(addr, errOpt.Value())
	}

	resE := s.Unlock(addr - offset)
	if resE.Error != nil {
		return ungo.NewException(addr, resE.Error)
	}
	return ungo.NewException(resE.Value+offset, nil)
}

func (cs *CombinedStorage) ReadFull(ptr MemoryPointer) []byte {
	cs.rwlock.Lock()
	defer cs.rwlock.Unlock()

	s, offset, errOpt := cs.findStorageAndOffset(ptr.Addr)
	if errOpt.HasValue() {
		return nil
	}

	localPtr := MemoryPointer{
		Addr:        ptr.Addr - offset,
		AllocHeader: ptr.AllocHeader,
	}
	return s.ReadFull(localPtr)
}

func (cs *CombinedStorage) Read(ptr MemoryPointer, readSize int, offset int) []byte {
	cs.rwlock.Lock()
	defer cs.rwlock.Unlock()

	s, storageOffset, errOpt := cs.findStorageAndOffset(ptr.Addr)
	if errOpt.HasValue() {
		return nil
	}

	localPtr := MemoryPointer{
		Addr:        ptr.Addr - storageOffset,
		AllocHeader: ptr.AllocHeader,
	}
	return s.Read(localPtr, readSize, offset)
}

func (cs *CombinedStorage) Write(ptr MemoryPointer, data []byte, offset int) {
	cs.rwlock.Lock()
	defer cs.rwlock.Unlock()

	s, storageOffset, errOpt := cs.findStorageAndOffset(ptr.Addr)
	if errOpt.HasValue() {
		return
	}

	localPtr := MemoryPointer{
		Addr:        ptr.Addr - storageOffset,
		AllocHeader: ptr.AllocHeader,
	}
	s.Write(localPtr, data, offset)
}

func (cs *CombinedStorage) Close() {
	cs.rwlock.Lock()
	defer cs.rwlock.Unlock()
	for _, s := range cs.storages {
		s.Close()
	}
}
func (cs *CombinedStorage) Pointer(addr MemAddr) ungo.Exception[MemoryPointer] {
	cs.rwlock.Lock()
	defer cs.rwlock.Unlock()

	s, offset, errOpt := cs.findStorageAndOffset(addr)
	if errOpt.HasValue() {
		return ungo.NewException(MemoryPointer{}, errOpt.Value())
	}

	ptrE := s.Pointer(addr - offset)
	if ptrE.Error != nil {
		return ungo.NewException(MemoryPointer{}, ptrE.Error)
	}

	translatedPtr := MemoryPointer{
		Addr:        ptrE.Value.Addr + offset,
		AllocHeader: ptrE.Value.AllocHeader,
	}
	return ungo.NewException(translatedPtr, nil)
}
