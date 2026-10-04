# allocator

并发安全的内存区间分配器，管理半开地址空间 `[0, Size)`，完整契约见 `SPEC.md`。

## 设计说明

### 索引结构

`Allocator` 持有两份冗余索引，由同一把 `sync.RWMutex` 保护：

- `byName map[string]Allocation`：按名字 O(1) 查找（`Lookup`、重名检测、`Free`）。
- `sorted []Allocation`：按 `(Start, Name)` 排序的切片（起始地址唯一），用于区间搜索与有序快照。

### 区间搜索

`findFree` 在排序切片上做一次线性间隙扫描：维护 `cursor`（当前间隙起点），对每个分配前的间隙 `[cursor, alloc.Start)` 以及尾部间隙 `[cursor, Size)`，用 `alignUp` 求间隙内最低对齐起点并检查 `length` 是否放得下，第一个命中的即全局最低可用起点。`overlaps` 用二分查找定位插入点，只检查左右相邻两个分配，O(log n)。

### 候选事务

`Apply` 分两阶段：

1. **结构校验**：对所有 op 按输入顺序做纯字段校验（不读状态），任一失败直接返回 `ErrInvalidInput`。
2. **候选执行**：克隆 `byName` 与 `sorted` 得到隔离候选状态，按输入顺序在其上执行；任何错误（冲突、空间不足、缺失释放、最终容量）直接丢弃候选，原状态、generation、revision 计数器完全不变。只有全部成功且最终数量不超 `MaxAllocations` 时才整体提交，并将 generation 加一（空批次不加）。

revision 由单调计数器 `nextRevision`（从 1 开始）在候选内分配，失败时计数器不落盘，因此回滚不消耗 revision。

### 算术安全

所有 `uint64` 加法先检查溢出：`Start+Length` 用 `Start > MaxUint64-Length` 判定；`alignUp` 在加 `alignment-1` 前检查上界；间隙适配用 `length > gapEnd-start`（减法无溢出）而非 `start+length <= gapEnd`。因此 `Size = MaxUint64`、对齐高达 `2^63` 等边界均安全。

### 实际复杂度

设 n 为当前分配数、batch 为批次大小：

- `Apply`：克隆 O(n)；每个 Reserve/Free O(n)（切片插入/删除搬移）或 O(log n)（重叠检查）；Allocate O(n) 间隙扫描。整体 O(n·(batch+1))，回滚零额外成本。
- `Find`：O(n)；`Lookup`：O(1)；`Snapshot`：O(n) 拷贝。
- 写操作互斥，读操作（`Find`/`Lookup`/`Snapshot`）共享读锁。

返回的切片均为独立拷贝，调用方修改不影响分配器内部状态。

## 使用

```go
a, _ := allocator.New(allocator.Options{Size: 1 << 20, MaxAllocations: 1024, MaxNameBytes: 64})
res, err := a.Apply(allocator.Batch{Ops: []allocator.Op{
    {Kind: allocator.Reserve, Name: "fixed", Start: 0, Length: 256},
    {Kind: allocator.Allocate, Name: "auto", Length: 128, Alignment: 64},
    {Kind: allocator.Free, Name: "fixed"},
}})
```

运行测试与演示：

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
