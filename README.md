# metacatalog286

并发安全的内存型元数据目录（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

### 索引
- 主索引为 `map[string]Record`，按名称 O(1) 定位记录。
- 另维护两个 O(1) 聚合计数器：`revision`（已分配的最大修订号）与
  `totalBytes`（Value 总字节数），避免 `Stats`/容量检查时全表扫描。
- `Snapshot`/`Result.Changed` 在返回前按名称排序，排序仅作用于拷贝出的切片。

### 候选事务（candidate transaction）
- `Apply` 先调用 `ValidateBatch` 做完整结构预检（kind、名称字符集与长度、
  单值字节上限、Delete 不得携带 Value），此阶段不读取任何状态。
- 随后在写锁内把当前 `records` 拷贝为候选 map，按输入顺序在其上执行
  Put/Delete：Put 使 revision 连续递增，Delete 不分配 revision；
  Delete 缺失键返回 `ErrNotFound`。
- 最终记录数与 Value 总字节容量只在批次末检查，超限返回 `ErrCapacity`。
- 任一步失败直接丢弃候选状态——store 的 map、generation、revision、
  totalBytes 均未被触碰，天然完成回滚；成功则整体交换候选状态，
  非空批次 generation 恰好 +1，空批次不变。

### 所有权
- 所有进出边界都做深拷贝：Put 时拷贝调用方传入的 Value；
  `Get`/`Snapshot`/`Result.Changed` 返回拷贝；`Clone` 逐条拷贝记录。
- 返回的切片与内部状态完全隔离，调用方修改返回值不会影响目录。

### 并发与复杂度
- 单把 `sync.RWMutex`：写操作（`Apply`）独占，读操作
  （`Get`/`Snapshot`/`Stats`/`Clone`/`ValidateBatch`）共享读锁，
  `Stats` 因此是线性一致的一致快照。
- `ValidateBatch`：O(batch)；`Apply`：O(n + batch)（n 为当前记录数，
  候选拷贝）+ O(c log c)（c 为变更数，排序）；`Get`：O(1)；
  `Snapshot`/`Clone`：O(n log n) / O(n)；`Stats`：O(1)。

## 文件分工
- `servicecatalog.go`：公开类型、错误值、`New`/`Apply`/`Get`/`Snapshot` 与候选事务。
- `validation.go`：无副作用的结构预检，`Apply` 与 `ValidateBatch` 共享同一套语义。
- `stats.go`：线性一致的状态统计。
- `clone.go`：保留逻辑时钟（generation/revision）且所有权完全隔离的深拷贝。

## 验证
```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
