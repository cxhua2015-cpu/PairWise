# resourcecatalog086

并发安全的内存型资源目录（Go 1.22+，仅标准库）。原子批次按输入顺序执行
Put/Delete，Put 分配连续 revision，Delete 不分配；失败整体回滚。

## 设计说明

**索引**：`Store` 以 `map[string]entry` 作为主索引（名称 → 值/revision），
Get 与批次内查找均为 O(1) 均摊。单把 `sync.RWMutex` 保护全部状态：
Apply 持写锁，Get/Snapshot 持读锁，因此所有公开方法可并发调用。

**候选事务**：Apply 分三个阶段——(1) 对全部 Op 做完整结构校验
（kind、名称字符集与长度、Value 长度），此阶段不读取任何状态；
(2) 在克隆出的候选 map 上按输入顺序执行 Put/Delete，Delete 缺失即
`ErrNotFound`；(3) 仅在批次末检查最终记录数与 Value 总字节容量，
超限返回 `ErrCapacity`。任一阶段失败都直接丢弃候选 map，原 map、
generation 与 revision 保持不变，天然回滚。非空成功批次 generation
只 +1，revision 按 Put 次数连续分配；空批次不改变任何计数。

**所有权**：Put 时深拷贝 Value 存入；Get/Snapshot/Result.Changed 返回的
Value 与 Records 切片均为新分配的副本，调用方对返回数据的修改不会
影响内部状态，反之亦然。Snapshot 记录按名称排序。

**复杂度**：Apply 为 O(B + N)，B 为批次数、N 为当前记录数（候选克隆与
容量合计）；Get 为 O(1)；Snapshot 为 O(N log N)（排序）。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
