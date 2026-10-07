# metacatalog321

并发安全的内存型元数据目录（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计

### 索引
- 主索引为 `map[string]entry`，`entry` 保存 `Value` 的私有副本与单调递增的 `Revision`。
- 另维护 `totalBytes`（所有 Value 字节数之和）作为冗余计数，避免每次容量检查时遍历索引。
- `Snapshot` 与 `Result.Changed` 按需对键排序（`sort.Strings`），索引本身无序。

### 候选事务
- `Apply` 分三阶段：
  1. **结构校验**：在不读取任何状态的情况下校验全部 op 的 kind、名称字符集/长度、Value 长度；任何错误返回 `ErrInvalidInput`。
  2. **候选执行**：克隆索引得到候选 map，按输入顺序执行 Put/Delete；Put 分配连续 revision 并写候选，Delete 不分配 revision、要求记录存在（否则 `ErrNotFound`）。
  3. **批次末容量检查**：仅对最终状态检查记录数与 Value 总字节数（`ErrCapacity`）。
- 任何失败直接丢弃候选，原始索引、`generation`、`revision` 完全不变（天然回滚）；成功则整体替换索引，`generation` 只增加一次（空批次不变）。

### 所有权
- Put 时深拷贝调用方的 `Value`；`Get`/`Snapshot`/`Result.Changed` 返回的 `Value` 与记录切片均为新分配的副本，调用方对返回值的任何修改不影响内部状态，反之亦然。

### 并发与复杂度
- 单把 `sync.RWMutex`：`Apply` 持写锁，`Get`/`Snapshot` 持读锁，所有公开方法可并发调用（`go test -race` 通过）。
- 设批次大小为 `B`、记录数为 `N`、结果/快照记录数为 `K`：
  - `Apply`：结构校验 `O(B)`，候选克隆 `O(N)`，执行 `O(B)`，结果排序 `O(K log K)`。
  - `Get`：`O(1)`（不计返回值拷贝的字节数）。
  - `Snapshot`：`O(N log N)` 排序 + `O(N)` 拷贝。
  - 空间：`O(N)` 索引 + `Apply` 期间 `O(N)` 候选副本。

## 使用

```go
s, _ := metacatalog321.New(metacatalog321.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
r, _ := s.Apply(metacatalog321.Batch{Ops: []metacatalog321.Op{{Kind: metacatalog321.Put, Name: "alpha", Value: []byte("v")}}})
```

运行演示：`go run ./cmd/demo`；测试：`go test ./...`、`go test -race ./...`。
