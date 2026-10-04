# reservation

并发安全的内存区间预约账本（Go 1.22+，仅标准库）。完整合同见 `SPEC.md`。

## 索引结构

`Ledger` 内部维护两份索引，由一把 `sync.RWMutex` 保护（写操作独占，读操作共享）：

- `byID map[string]*Reservation`：全局 ID 索引，用于 Add 判重、Delete 定位，O(1)。
- `byResource map[string][]*Reservation`：每个资源一个按 `(Start, End, ID)` 排序的切片。
  由于同一资源内区间互不重叠，按 `Start` 排序即等价于按 `(Start,End,ID)` 排序，
  支持二分查找定位。

## 半开区间

所有区间为半开 `[Start,End)`：`At(t)` 命中当且仅当 `Start <= t < End`（`t == End` 不覆盖）；
`Scan(from,to)` 返回满足 `Start < to && End > from` 的预约。相邻区间（前者的 `End` 等于
后者的 `Start`）不算重叠。`Start`、`End` 支持任意 int64 值（含 `math.MinInt64/MaxInt64`），
所有比较均为纯有序比较，不做减法，无溢出风险。

## 事务（ApplyBatch）

1. **结构校验**：按输入顺序校验全部 change（类型、Add 不得带 `Change.ID`、Delete 不得带
   `Reservation.ID`、ID/Resource 字符集与长度、`Start < End`、单 Value ≤ 1 MiB），
   任一失败立即返回，不做任何语义处理。
2. **语义执行**：在候选副本（索引的浅拷贝 + 新增预约的深拷贝）上按输入顺序执行；
   Add 已存在 ID 返回 `ErrDuplicate`，Delete 不存在 ID 返回 `ErrNotFound`，
   批内允许 Delete 后 Add 同一 ID（替换语义）。
3. **最终状态检查**：仅在全部语义操作完成后检查最终状态——资源数、预约数、Value 总字节
   （`ErrCapacity`），以及同一资源内重叠（`ErrConflict`）。失败则丢弃候选副本，零副作用，
   generation 不变。
4. 非空成功批次 generation 恰好加一；空批次只读地返回当前 generation。

## 冲突检测与容量

候选状态按资源内有序切片线性扫描相邻对即可发现重叠（`next.Start < prev.End` 即冲突），
无需区间树。容量统计（预约数、Value 总字节）随语义操作增量维护，最终与上限比较。

## 所有权隔离

- 输入：Add 的 `Value` 在入库前深拷贝，调用方之后修改入参不影响账本。
- 输出：`At`/`Scan`/`Snapshot` 返回的每个 `Value` 都是新分配的副本，彼此及与内部状态
  完全隔离，调用方可自由修改。

## 实际复杂度

设 n 为单资源预约数，N 为总预约数，R 为资源数，B 为批大小，k 为 Scan 命中数：

- `At`：O(log n)
- `Scan`：O(log n + k)
- `Snapshot`：O(N + R log R)（含全部 Value 拷贝）
- `ApplyBatch`：结构校验 O(B)；候选拷贝 O(N)；语义操作 O(B · n)（切片插入/删除为线性移动）；
  最终检查 O(N)。即整体 O(N + B·n)。这是简单正确性优先的实现：回滚靠丢弃候选副本完成，
  无需undo日志；如需支撑超大账本高频写入，可换成持久化/不可变数据结构或区间树。

## 并发

所有公开方法均可并发调用：写（`Apply`/`ApplyBatch`）持写锁，读（`At`/`Scan`/`Snapshot`）
持读锁。`go test -race ./...` 通过。
