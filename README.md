# resourcecatalog081

并发安全的内存型资源目录（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

**索引**
- 主索引为 `map[string]Record`，以名称为主键，Get/Apply 均为 O(1) 定位。
- 无次级索引；Snapshot 与 Result.Changed 在返回前按名称排序（O(n log n)）。

**候选事务（candidate transaction）**
- `Apply` 分三阶段：
  1. 结构校验：对整个批次做纯函数式校验（kind 合法、名称字符集与长度、Value 长度），不读取任何状态，失败返回 `ErrInvalidInput`。
  2. 候选执行：在持锁状态下克隆当前 map 得到候选副本，按输入顺序应用 Put/Delete；Put 在候选 revision 计数器上分配连续 revision，Delete 不分配；删除不存在的名称返回 `ErrNotFound`。
  3. 批次末容量检查：仅对最终候选状态检查记录数与 Value 总字节数，超限返回 `ErrCapacity`。
- 任一阶段失败直接丢弃候选，已提交状态、generation、revision 完全不变（天然回滚）；成功时整体替换 map，非空批次 generation 恰好 +1。

**所有权**
- Put 时深拷贝 `Op.Value`；Get/Snapshot 返回深拷贝的 Value。调用方对返回切片的任何修改、以及对入参切片的后续修改，都不会影响内部状态，反之亦然。
- `Result.Changed` 按名称排序，记录每个被触及名称的最终状态；Delete 产生的条目 Value 为 nil、Revision 为 0。

**并发与复杂度**
- 单把 `sync.RWMutex`：Apply 持写锁，Get/Snapshot 持读锁，所有公开方法可并发调用（`-race` 验证）。
- 设批次长度为 b、当前记录数为 n：Apply 为 O(b + n)（候选克隆 O(n)）；Get 为 O(1)；Snapshot 为 O(n log n)。

## 验证

```
go test ./...
go test -race ./...
go run ./cmd/demo
```
