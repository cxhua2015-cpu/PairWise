# metacatalog341

并发安全的内存型元数据目录（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

### 索引
- 主索引为 `map[string]Record`，按名称 O(1) 定位；名称即主键。
- 另维护两个派生计数器：当前记录数（`len(records)`）与 Value 总字节数 `total`，随每次 Put/Delete 增量更新，避免批次末 O(n) 全量扫描求和。
- `revision`（单调递增、仅 Put 分配）与 `generation`（非空成功批次 +1）为标量状态。

### 候选事务（candidate transaction）
`Apply` 分两阶段：
1. **结构校验**：在读取任何状态前，完整校验所有 Op 的 kind、名称字符集/长度、Value 长度；任一失败即返回 `ErrInvalidInput`，不触碰状态。
2. **候选执行**：在写锁内把当前索引浅拷贝为候选 map，按输入顺序在其上应用 Put/Delete（Put 深拷贝 Value 并分配连续 revision，Delete 不分配 revision、缺失返回 `ErrNotFound`）。记录数与 Value 总字节容量只在批次末检查，超限返回 `ErrCapacity`。
3. **提交或回滚**：成功则整体用候选 map 替换主索引并推进 generation/revision；任何失败直接丢弃候选，主索引、generation、revision 均不变——回滚是“零成本”的，无需反向补偿。

### 所有权
- 入参 `Op.Value` 在 Put 时深拷贝，调用方之后修改入参切片不影响目录。
- `Get`/`Snapshot`/`Result.Changed` 返回的 Value 均为深拷贝，返回切片与内部状态完全隔离。
- `Snapshot.Records` 与 `Result.Changed` 按名称排序，保证输出确定性。

### 并发
- 单把 `sync.RWMutex` 保护全部状态：`Apply` 取写锁，`Get`/`Snapshot` 取读锁。
- 结构校验在加锁前完成（只依赖不可变的 Options），缩短临界区。

### 复杂度
- `New`：O(1)。
- `Apply`（k 个 Op，n 条现存记录）：结构校验 O(k·L)（L 为名称长度）；候选拷贝 O(n)；应用 O(k)；末检 O(1)；结果排序 O(k log k)。总体 O(n + k log k)。
- `Get`：O(1) 平均（外加 O(V) 的 Value 拷贝）。
- `Snapshot`：O(n log n)（排序 + 深拷贝）。

## 使用

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
