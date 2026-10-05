# inventory

并发安全的内存型库存计数账本（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

### 索引
账本只维护一个主索引：`map[string]Account`，以账户名为键，存储当前值与最近
revision。`Top` 与 `Snapshot` 不维护辅助有序索引，而是在读取时把 map 拷贝成切片
后排序——账户数在控制面场景下有限，这样可避免写路径上的额外索引维护成本，同时
天然保证返回切片与内部状态隔离（每次返回的都是新拷贝）。

### 候选事务（candidate transaction）
`Apply` 先在持锁状态下对整个批次做完整结构校验（kind 合法、名称字符集与字节上
限），再将主索引浅拷贝为候选 map，在候选上按输入顺序执行 Add/Set/Delete：
Add/Set 各分配一个连续 revision，Delete 不分配。int64 溢出在算术前检测，绝对值
上限逐操作检查，账户容量上限仅在批次末对候选大小检查。任一步失败直接丢弃候选，
主状态、generation、revision 完全不变（整体回滚）；全部成功则一次性用候选替换
主索引，generation 恰好加一。空批次为成功空操作，不改变 generation。

### 所有权
`Ledger` 内部状态（map、generation、revision）完全由 `Ledger` 所有，受一把
`sync.RWMutex` 保护：`Apply` 取写锁，`Top`/`Snapshot` 取读锁。所有返回的切片
（`Result.Changed`、`Top`、`Snapshot.Accounts`）都是新建拷贝，调用方修改不会影
响内部状态；`Account`/`Result`/`Snapshot` 均为纯值类型，无共享指针。

### 复杂度
设批次含 b 个操作、当前 n 个账户、Top 取 k 项：
- `Apply`：校验 O(b·名称长度)，候选拷贝 O(n)，执行 O(b)，合计 O(n + b)。
- `Top`：O(n log n) 排序后取前 k 项。
- `Snapshot`：O(n log n) 按名称排序。
- 空间：每次成功 `Apply` 额外 O(n) 候选拷贝，失败时随函数返回被回收。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
