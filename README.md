# allocator

并发安全的内存区间分配器，管理半开地址空间 `[0, Size)`，仅依赖 Go 标准库（Go 1.22+）。公开契约见 `SPEC.md`。

## 用法

```go
a, _ := allocator.New(allocator.Options{Size: 64, MaxAllocations: 8, MaxNameBytes: 16})
res, _ := a.Apply(allocator.Batch{Ops: []allocator.Op{
    {Kind: allocator.Reserve, Name: "fixed", Start: 0, Length: 8},
    {Kind: allocator.Allocate, Name: "auto", Length: 4, Alignment: 8},
}})
snap := a.Snapshot()
```

运行演示：`go run ./cmd/demo`；测试：`go test ./...`、`go test -race ./...`。

## 设计说明

### 索引结构

分配器维护两份冗余索引，由同一把 `sync.Mutex` 保护：

- `byName map[string]Allocation`：按名称 O(1) 查重与 `Lookup`。
- `sorted []Allocation`：按 `(Start, Name)` 排序的区间切片，用于重叠检测、最低空位搜索和有序 `Snapshot`。

插入/删除用二分查找定位（`sort.Search`），切片移动元素；`Snapshot` 直接复制该有序切片，无需再排序。

### 区间搜索

`findLoc` 在有序区间上做单次线性扫描：维护 `gapStart`（当前空隙起点，随已扫描区间的末端单调推进），对每个空隙计算第一个满足对齐的起点 `s = ceil(gapStart/alignment)*alignment`，若 `[s, s+length)` 落入空隙则立即返回——因为空隙按起点升序、每个空隙内取最低对齐点，第一个命中即全局最低可用区间。`Allocate` 与 `Find` 共用该函数，语义一致。

### 候选事务

`Apply` 分两阶段：

1. **结构校验**：按输入顺序逐个校验所有操作（名称字符集、字段约束、`[Start, Start+Length)` 落在地址空间内），完全不读状态，任何失败直接返回 `ErrInvalidInput`。
2. **候选执行**：把 `byName`/`sorted`/`nextRev` 拷贝到候选状态，按输入顺序执行；Reserve/Allocate 分配连续 revision，Free 不分配。任何冲突（`ErrConflict`/`ErrNoSpace`/`ErrNotFound`）或最终数量超过 `MaxAllocations`（`ErrCapacity`）都直接丢弃候选——原始状态、generation、revision 计数天然不变，无需显式回滚。全部成功才一次性提交，非空批次 generation 加一。

因此同批"先释放再分配""删除后复用名称""临时超量但最终合规"都能在候选上正确求值，而失败批次不消耗 revision。

### 算术安全

所有 `uint64` 运算避免溢出：

- 区间端点用 `Length > Size - Start` 形式比较，从不计算 `Start + Length`。
- 对齐上取整先算 `delta = alignment - gapStart%alignment`，并检查 `delta > MaxUint64 - gapStart` 的溢出情形。
- 空隙容纳判断用 `length > gapEnd - s`，不做加法。

### 并发与返回值隔离

所有公开方法在同一互斥锁下执行，写操作（`Apply`）与只读操作（`Find`/`Lookup`/`Snapshot`）互斥，`-race` 下无数据竞争。`Snapshot`/`Result.Changed` 返回的切片均为新分配的副本，调用方修改不影响分配器内部状态。

### 实际复杂度

设 `n` 为当前分配数、`b` 为批次数：

- `Apply`：校验 O(b·L)（L 为名称长度）；候选拷贝 O(n)；执行 O(b·n)（每次插入/重叠检测为二分加线性移动）；整体 O(n + b·n)。
- `Find` / 单次 `Allocate` 搜索：O(n)。
- `Lookup`：O(1) 均摊。
- `Snapshot`：O(n)。
- 空间：O(n)。

该实现面向控制平面场景（分配数量中等、正确性与可回滚性优先），未引入平衡树等复杂结构。
