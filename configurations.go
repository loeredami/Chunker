package chunker

const (
	DefaultChunkSize    = 32
	DefaultMiniMemSize  = 10 * MB
	DefaultMiniSwapSize = 20 * MB
	DefaultMemSize      = 2 * GB
	DefaultSwapSize     = 4 * GB
)

func NewDefaultMiniMemoryConfig() *Memory {
	return NewMemory(DefaultMiniMemSize, DefaultChunkSize)
}

func NewDefaultMemoryConfig() *Memory {
	return NewMemory(DefaultMemSize, DefaultChunkSize)
}

func NewDefaultMiniSwapConfig(filePath string) (*SwapMemory, error) {
	return NewSwapMemory(filePath, DefaultMiniSwapSize, DefaultChunkSize)
}

func NewDefaultSwapConfig(filePath string) (*SwapMemory, error) {
	return NewSwapMemory(filePath, DefaultSwapSize, DefaultChunkSize)
}

func NewDefaultMiniMemOverSwapConfig(filePath string) (*CombinedStorage, error) {
	mem := NewMemory(DefaultMiniMemSize, DefaultChunkSize)
	swap, err := NewSwapMemory(filePath, DefaultMiniSwapSize, DefaultChunkSize)
	if err != nil {
		return nil, err
	}
	return NewCombinedStorage(mem, swap), nil
}

func NewDefaultMemOverSwapConfig(filePath string) (*CombinedStorage, error) {
	mem := NewMemory(DefaultMemSize, DefaultChunkSize)
	swap, err := NewSwapMemory(filePath, DefaultSwapSize, DefaultChunkSize)
	if err != nil {
		return nil, err
	}
	return NewCombinedStorage(mem, swap), nil
}

func NewDefaultMiniSwapOverMemConfig(filePath string) (*CombinedStorage, error) {
	swap, err := NewSwapMemory(filePath, DefaultMiniSwapSize, DefaultChunkSize)
	if err != nil {
		return nil, err
	}
	mem := NewMemory(DefaultMiniMemSize, DefaultChunkSize)
	return NewCombinedStorage(swap, mem), nil
}

func NewDefaultSwapOverMemConfig(filePath string) (*CombinedStorage, error) {
	swap, err := NewSwapMemory(filePath, DefaultSwapSize, DefaultChunkSize)
	if err != nil {
		return nil, err
	}
	mem := NewMemory(DefaultMemSize, DefaultChunkSize)
	return NewCombinedStorage(swap, mem), nil
}
