# costledger

并发安全的内存型成本计量账本（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

**索引**
- 主索引为 `map[string]Account`（名称 → 账户），Add/Set/Delete 均为 O(1) 定位。
- 不维护持久有序结构；`Top` 与 `Snapshot` 在读取时拷贝并排序，以换取写入路径的低开销与实现的简单性。

**候选事务（candidate transaction）**
- `Apply` 分两阶段：先对整个批次做纯结构校验（kind、未使用字段必须为零、名称字符集与长度），不读取任何状态。
- 通过后，在写锁内把当前账户表浅拷贝为候选 map，按输入顺序在其上执行全部操作；任何一步失败（溢出/绝对值上限 `ErrValue`、删除不存在 `ErrNotFound`、批次末容量 `ErrCapacity`）直接丢弃候选，状态零变更——回滚即“不提交”。
- 全部成功且最终账户数不超限才用候选整体替换正式表，generation 恰好加一，revision 连续分配；空批次不改变 generation。

**所有权与并发**
- 所有公开方法由一把 `sync.RWMutex` 保护：`Apply` 取写锁，`Top`/`Snapshot` 取读锁。
- 返回的切片（`Result.Changed`、`Top`、`Snapshot.Accounts`）均为新分配的拷贝，调用方修改不会影响内部状态；内部 map 只在持锁时访问。

**复杂度**（n = 账户数，b = 批次数）
- `Apply`：结构校验 O(b)；候选拷贝 O(n)；执行 O(b)；总计 O(n + b) 时间、O(n) 额外空间。
- `Top(k)`：O(n log n) 排序后取前 k，O(n) 额外空间。
- `Snapshot`：O(n log n)（按名称排序），O(n) 额外空间。
- `New`：O(1)。

## 验证

```
go test ./...
go test -race ./...
go run ./cmd/demo
```
